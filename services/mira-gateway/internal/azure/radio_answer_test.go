package azure

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dcasati/mira/internal/foundryiq"
	"github.com/gorilla/websocket"
)

func lookupTestSession(t *testing.T, ctx context.Context, runner *foundryiq.ToolRunner) (*RealtimeSession, <-chan map[string]any) {
	t.Helper()
	messages := make(chan map[string]any, 8)
	realtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for {
			var message map[string]any
			if err := conn.ReadJSON(&message); err != nil {
				return
			}
			select {
			case messages <- message:
			case <-ctx.Done():
				return
			}
		}
	}))
	t.Cleanup(realtime.Close)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(realtime.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &RealtimeSession{
		conn: conn, events: make(chan realtimeEvent, 2), filler: "Standby.",
		foundryIQ: runner, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, messages
}

func TestNormalizeRadioResultSeparatesSummaryAndRetainsDetail(t *testing.T) {
	for _, answer := range []string{
		"SPOKEN: Markus Long, until eight AM tomorrow.\nDETAIL: Full dates and timezone.",
		" spoken: Markus Long, until eight AM tomorrow.\r\n detail: Full dates and timezone. ",
		"**SPOKEN:** Markus Long, until eight AM tomorrow.\n**DETAIL:** Full dates and timezone.",
		"**SPOKEN**: Markus Long, until eight AM tomorrow. **DETAIL**: Full dates and timezone.",
		"```text\nSPOKEN: Markus Long, until eight AM tomorrow.\nDETAIL: Full dates and timezone.\n```",
	} {
		t.Run(answer, func(t *testing.T) {
			input, _ := json.Marshal(foundryiq.QueryResult{
				Answer: answer, ResponseID: "r1", AgentName: "router",
			})
			output, voice, err := normalizeRadioResult("query_foundry_iq_manuals", string(input))
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]string
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			if voice != "Markus Long, until eight AM tomorrow." || result["answer"] != voice {
				t.Fatalf("wrong speech content: %v", result)
			}
			if result["follow_up_context"] != "Full dates and timezone." ||
				result["response_id"] != "r1" || result["agent_name"] != "router" {
				t.Fatalf("lost follow-up or metadata: %v", result)
			}
			if strings.Contains(strings.ToUpper(output), "SPOKEN") || strings.Contains(output, "DETAIL:") {
				t.Fatalf("presentation labels leaked: %s", output)
			}
		})
	}
}

func TestNormalizeRadioResultPreservesNonRouterAuthorities(t *testing.T) {
	input := `{"answer":"SPOKEN: source text\nDETAIL: retain verbatim"}`
	for _, name := range []string{"get_device_telemetry", "send_zello_chat_message", "other_tool"} {
		got, voice, err := normalizeRadioResult(name, input)
		if err != nil || got != input || voice != "" {
			t.Fatalf("changed %s evidence: %q %q %v", name, got, voice, err)
		}
	}
	plain := `{"answer":"The procedure explains spoken communication. Preserve all qualifications."}`
	got, voice, err := normalizeRadioResult("query_foundry_iq_manuals", plain)
	if err != nil || got != plain || voice != "" {
		t.Fatalf("changed ordinary prose: %q %q %v", got, voice, err)
	}
}

func TestNormalizeRadioResultHandlesSummaryOnlyAndRejectsBrokenEnvelope(t *testing.T) {
	output, voice, err := normalizeRadioResult("query_foundry_iq_manuals", `{"answer":"SPOKEN: No coverage."}`)
	if err != nil || voice != "No coverage." || strings.Contains(output, "SPOKEN:") {
		t.Fatalf("summary-only result: %q %q %v", output, voice, err)
	}
	for _, answer := range []string{"SPOKEN:", "SPOKEN: \nDETAIL: full", "SPOKEN: short\nDETAIL:"} {
		input, _ := json.Marshal(foundryiq.QueryResult{Answer: answer})
		if _, _, err := normalizeRadioResult("query_foundry_iq_manuals", string(input)); err == nil {
			t.Fatalf("accepted incomplete presentation: %s", answer)
		}
	}
}

func TestRadioPresentationReachesRealtimeWithoutLabels(t *testing.T) {
	for _, mode := range []string{"audio", "text"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{
					"id": "r1", "output_text": "SPOKEN: Markus Long, until eight AM tomorrow.\nDETAIL: Ends September 11, 08:00 MDT.",
				})
			}))
			defer backend.Close()
			client, err := foundryiq.NewClient(foundryiq.Config{DirectEndpoint: backend.URL}, nil)
			if err != nil {
				t.Fatal(err)
			}
			session, messages := lookupTestSession(t, ctx, foundryiq.NewToolRunner(client))
			calls := []functionCall{{Name: "query_foundry_iq_manuals", CallID: "c1", Arguments: `{"question":"Who is on-call?"}`}}
			err = session.runFunctionsAndRespond(ctx, calls, mode, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{"conversation.item.create", "response.create"} {
				select {
				case message := <-messages:
					if message["type"] != expected {
						t.Fatalf("unexpected event: %v", message)
					}
					encoded, _ := json.Marshal(message)
					if strings.Contains(string(encoded), "SPOKEN:") || strings.Contains(string(encoded), "DETAIL:") {
						t.Fatalf("raw labels reached Realtime: %s", encoded)
					}
					if expected == "conversation.item.create" {
						var result map[string]string
						item := message["item"].(map[string]any)
						if err := json.Unmarshal([]byte(item["output"].(string)), &result); err != nil {
							t.Fatal(err)
						}
						if result["follow_up_context"] != "Ends September 11, 08:00 MDT." {
							t.Fatalf("follow-up not retained: %v", result)
						}
					} else if mode != "text" {
						response := message["response"].(map[string]any)
						if response["tool_choice"] != "none" ||
							!strings.Contains(response["instructions"].(string), "Markus Long") ||
							strings.Contains(response["instructions"].(string), "September 11") {
							t.Fatalf("speech request must select summary, not detail: %v", response)
						}
					}
				case <-ctx.Done():
					t.Fatal("missing Realtime protocol event")
				}
			}
		})
	}
}

func TestMultipleRadioResultsKeepNormalReasoningPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, messages := lookupTestSession(t, ctx, nil)
	if err := session.createToolResponse("audio", []functionOutput{
		{voiceAnswer: "One answer"}, {voiceAnswer: "Another answer"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-messages:
		response := message["response"].(map[string]any)
		if _, exists := response["input"]; exists {
			t.Fatalf("must retain conversation context for multiple results: %v", message)
		}
		if _, exists := response["tool_choice"]; exists {
			t.Fatalf("must keep normal reasoning for multiple results: %v", message)
		}
		if !strings.Contains(response["instructions"].(string), "Never offer more detail") {
			t.Fatalf("dispatcher instructions missing from normal audio response: %v", message)
		}
	case <-ctx.Done():
		t.Fatal("missing response event")
	}
}
