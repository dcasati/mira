package azure

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func speechPolicySession(t *testing.T) (*RealtimeSession, <-chan map[string]any, *bytes.Buffer) {
	t.Helper()
	messages := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			t.Error(err)
			return
		}
		messages <- message
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	logs := &bytes.Buffer{}
	return &RealtimeSession{
		conn: conn, events: make(chan realtimeEvent, 2), filler: "Standby.",
		logger: slog.New(slog.NewJSONHandler(logs, nil)),
	}, messages, logs
}

func TestLookupFillerRejectsUnexpectedSpeech(t *testing.T) {
	for _, tc := range []struct {
		name       string
		transcript string
		wantAudio  bool
	}{
		{"exact", "Standby.", true},
		{"spacing_and_case", "STAND BY!", true},
		{"chatty_lookup", "I'm checking this information for you now, it will just take a moment.", false},
		{"added_offer", "Standby. If you need any more details let me know.", false},
		{"missing_transcript", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			session, messages, logs := speechPolicySession(t)
			response, err := json.Marshal(map[string]any{
				"output": []any{map[string]any{
					"content": []any{map[string]string{"transcript": tc.transcript}},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			session.events <- realtimeEvent{Type: "response.output_audio.delta", Delta: base64.StdEncoding.EncodeToString([]byte{1, 0})}
			session.events <- realtimeEvent{Type: "response.done", Response: response}
			transmitted := false
			err = session.sayLookupFiller(ctx, func(context.Context, []int16, int) error {
				transmitted = true
				return nil
			})
			if err != nil || transmitted != tc.wantAudio {
				t.Fatalf("transmitted=%v want=%v err=%v", transmitted, tc.wantAudio, err)
			}
			if !tc.wantAudio && !strings.Contains(logs.String(), "azure.lookup_filler_rejected") {
				t.Fatal("rejected acknowledgement must be logged")
			}
			select {
			case message := <-messages:
				request := message["response"].(map[string]any)
				input, ok := request["input"].([]any)
				if !ok || len(input) != 0 || request["conversation"] != "none" || request["tool_choice"] != "none" {
					t.Fatalf("filler must be isolated from conversation and tools: %v", request)
				}
				if !strings.Contains(request["instructions"].(string), `Speak only this exact phrase: "Standby."`) {
					t.Fatalf("filler must request only the configured phrase: %v", request)
				}
			case <-ctx.Done():
				t.Fatal("missing filler request")
			}
		})
	}
}

func TestDispatcherPromptPreservesQualificationsAndExplicitFollowup(t *testing.T) {
	for _, required := range []string{
		"20 words or fewer", "Answer the question, then stop.",
		"Preserve safety warnings, uncertainty, scope, and required approvals",
		"Never offer more detail or another task unprompted.",
		"acknowledgements, not requests for more detail",
	} {
		if !strings.Contains(SystemPrompt, required) {
			t.Errorf("dispatcher prompt missing %q", required)
		}
	}
	for _, required := range []string{
		"Keep follow_up_context silently for later",
		"explicitly requests more on the SAME topic",
		"without another lookup",
		`An acknowledgement alone ("yes", "copy", "roger", "thanks") is not a request for more.`,
	} {
		if !strings.Contains(foundryIQPrompt, required) {
			t.Errorf("lookup prompt missing %q", required)
		}
	}
}

func TestAudioRequestsCarryProfileInstructions(t *testing.T) {
	for _, mode := range []string{"normal", "formatted"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			session, messages, _ := speechPolicySession(t)
			var err error
			if mode == "normal" {
				err = session.createResponse("audio")
			} else {
				err = session.createToolResponse("audio", []functionOutput{{voiceAnswer: "Shift ends at eight."}})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case message := <-messages:
				request := message["response"].(map[string]any)
				instructions := request["instructions"].(string)
				if !strings.HasPrefix(instructions, session.instructions()) {
					t.Fatal("response override must preserve profile instructions")
				}
				if mode == "normal" {
					if _, exists := request["input"]; exists {
						t.Fatal("ordinary answers must retain conversation context")
					}
				} else {
					input, ok := request["input"].([]any)
					if !ok || len(input) != 0 || request["tool_choice"] != "none" {
						t.Fatal("formatted speech must use only supplied answer")
					}
					if _, exists := request["conversation"]; exists {
						t.Fatal("final answer must remain in default conversation")
					}
				}
			case <-ctx.Done():
				t.Fatal("missing response request")
			}
		})
	}
}
