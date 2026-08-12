package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/dcasati/mira/internal/audio"
	"github.com/gorilla/websocket"
)

const SystemPrompt = `You are MIRA, the Mobile Intelligence Radio Assistant.

You communicate with a family through a push-to-talk Zello radio channel.

Keep spoken responses concise, natural, useful, and easy to understand over radio.
Prefer short conversational answers.
Avoid reading markdown, citations, URLs, tables, or unnecessary formatting aloud.
Give longer explanations only when explicitly requested.
Your name is MIRA.`

type RealtimeSession struct {
	id        string
	conn      *websocket.Conn
	events    chan realtimeEvent
	resampler audio.Resampler
	logger    *slog.Logger
	mu        sync.Mutex
}

type realtimeEvent struct {
	Type    string `json:"type"`
	Delta   string `json:"delta"`
	Session struct {
		ID string `json:"id"`
	} `json:"session"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	Response json.RawMessage `json:"response"`
}

type responseDone struct {
	Output []struct {
		Content []struct {
			Text       string `json:"text"`
			Transcript string `json:"transcript"`
		} `json:"content"`
	} `json:"output"`
}

func newRealtimeSession(ctx context.Context, cfg Config, cred azcore.TokenCredential, resampler audio.Resampler, logger *slog.Logger) (*RealtimeSession, error) {
	logger.Info("azure.connecting", "deployment", cfg.Deployment)
	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{TokenScope}})
	if err != nil {
		return nil, err
	}
	wsURL, err := realtimeURL(cfg.Endpoint, cfg.Deployment)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token.Token)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, err
	}
	s := &RealtimeSession{
		conn:      conn,
		events:    make(chan realtimeEvent, 128),
		resampler: resampler,
		logger:    logger,
	}
	go s.readLoop()
	if err := s.waitCreatedAndConfigure(ctx, cfg.Voice); err != nil {
		_ = conn.Close()
		return nil, err
	}
	logger.Info("azure.connected", "session_id", s.id)
	return s, nil
}

func realtimeURL(endpoint, deployment string) (string, error) {
	u, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported Azure OpenAI endpoint scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/openai/v1/realtime"
	q := u.Query()
	q.Set("model", deployment)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *RealtimeSession) ID() string { return s.id }

func (s *RealtimeSession) AskAudio(ctx context.Context, pcm []int16, sampleRate int) ([]int16, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	input, err := s.resampler.Resample(pcm, sampleRate, audio.AzureSampleRate)
	if err != nil {
		return nil, 0, err
	}
	if len(input) == 0 {
		return nil, audio.AzureSampleRate, nil
	}
	raw := audio.Int16ToBytes(input)
	const chunkBytes = 4800
	for start := 0; start < len(raw); start += chunkBytes {
		end := start + chunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		if err := s.sendJSON(map[string]any{
			"type":  "input_audio_buffer.append",
			"audio": base64.StdEncoding.EncodeToString(raw[start:end]),
		}); err != nil {
			return nil, 0, err
		}
	}

	if err := s.sendJSON(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		return nil, 0, err
	}
	if err := s.sendJSON(map[string]any{"type": "response.create"}); err != nil {
		return nil, 0, err
	}
	s.logger.Info("azure.response_started", "session_id", s.id)
	var output []byte
	for {
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return nil, 0, errors.New("azure realtime session closed")
			}
			switch ev.Type {
			case "response.output_audio.delta", "response.audio.delta":
				chunk, err := base64.StdEncoding.DecodeString(ev.Delta)
				if err != nil {
					return nil, 0, err
				}
				output = append(output, chunk...)
			case "response.done":
				s.logger.Info("azure.response_finished", "session_id", s.id, "bytes", len(output))
				samples, err := audio.BytesToInt16(output)
				if err != nil {
					return nil, 0, err
				}
				return samples, audio.AzureSampleRate, nil
			case "error":
				if ev.Error != nil {
					return nil, 0, fmt.Errorf("azure realtime error: %s", ev.Error.Message)
				}
				return nil, 0, errors.New("azure realtime error")
			}
		case <-time.After(60 * time.Second):
			return nil, 0, errors.New("azure realtime response timed out")
		}
	}
}

func (s *RealtimeSession) AskText(ctx context.Context, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	if err := s.sendJSON(map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]any{{
				"type": "input_text",
				"text": text,
			}},
		},
	}); err != nil {
		return "", err
	}
	if err := s.sendJSON(map[string]any{
		"type": "response.create",
		"response": map[string]any{
			"output_modalities": []string{"text"},
		},
	}); err != nil {
		return "", err
	}
	s.logger.Info("azure.text_response_started", "session_id", s.id)
	var out strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return "", errors.New("azure realtime session closed")
			}
			switch ev.Type {
			case "response.output_text.delta", "response.text.delta":
				out.WriteString(ev.Delta)
			case "response.done":
				if out.Len() == 0 {
					out.WriteString(extractResponseText(ev.Response))
				}
				reply := strings.TrimSpace(out.String())
				s.logger.Info("azure.text_response_finished", "session_id", s.id, "bytes", len(reply))
				return reply, nil
			case "error":
				if ev.Error != nil {
					return "", fmt.Errorf("azure realtime error: %s", ev.Error.Message)
				}
				return "", errors.New("azure realtime error")
			}
		case <-time.After(60 * time.Second):
			return "", errors.New("azure realtime text response timed out")
		}
	}
}

func (s *RealtimeSession) Close(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- s.conn.Close() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *RealtimeSession) waitCreatedAndConfigure(ctx context.Context, voice string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-s.events:
			if ev.Type == "session.created" {
				s.id = ev.Session.ID
				goto configure
			}
			if ev.Type == "error" && ev.Error != nil {
				return fmt.Errorf("azure realtime error: %s", ev.Error.Message)
			}
		case <-time.After(15 * time.Second):
			return errors.New("timed out waiting for Azure realtime session.created")
		}
	}
configure:
	if voice == "" {
		voice = "alloy"
	}
	if err := s.sendJSON(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type":              "realtime",
			"instructions":      SystemPrompt,
			"output_modalities": []string{"audio"},
			"audio": map[string]any{
				"input": map[string]any{
					"format": map[string]any{"type": "audio/pcm", "rate": audio.AzureSampleRate},
					"turn_detection": map[string]any{
						"type":                "server_vad",
						"threshold":           0.5,
						"prefix_padding_ms":   300,
						"silence_duration_ms": 500,
						"create_response":     false,
					},
				},
				"output": map[string]any{
					"voice":  voice,
					"format": map[string]any{"type": "audio/pcm", "rate": audio.AzureSampleRate},
				},
			},
		},
	}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-s.events:
			if ev.Type == "session.updated" {
				return nil
			}
			if ev.Type == "error" && ev.Error != nil {
				return fmt.Errorf("azure realtime error: %s", ev.Error.Message)
			}
		case <-time.After(15 * time.Second):
			return errors.New("timed out waiting for Azure realtime session.updated")
		}
	}
}

func (s *RealtimeSession) readLoop() {
	defer close(s.events)
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		var ev realtimeEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			s.logger.Warn("azure.event_decode_failed", "reason", err.Error())
			continue
		}
		select {
		case s.events <- ev:
		default:
			s.logger.Warn("azure.event_dropped", "type", ev.Type)
		}
	}
}

func (s *RealtimeSession) sendJSON(v any) error {
	return s.conn.WriteJSON(v)
}

func extractResponseText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var done responseDone
	if err := json.Unmarshal(raw, &done); err != nil {
		return ""
	}
	var out strings.Builder
	for _, item := range done.Output {
		for _, content := range item.Content {
			if content.Text != "" {
				out.WriteString(content.Text)
			} else if content.Transcript != "" {
				out.WriteString(content.Transcript)
			}
		}
	}
	return out.String()
}
