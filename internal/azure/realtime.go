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
	"github.com/dcasati/mira/internal/conversation"
	"github.com/dcasati/mira/internal/foundryiq"
	"github.com/dcasati/mira/internal/telemetry"
	"github.com/gorilla/websocket"
)

const SystemPrompt = `You are Operator, the Mobile Intelligence Radio Assistant.

You communicate over a push-to-talk Zello radio channel and behave like a calm, professional radio/phone operator.

Use concise radio procedure for spoken responses:
- Address the caller by their Zello name or call sign when known.
- Read back the caller name/call sign at the start of operational replies, for example: "wx-ops, Operator. Roger."
- Use standard prowords naturally: Roger, Wilco, Standby, Say again, Affirmative, Negative, Correction, I say again, Over, Out.
- Use "Over" when you expect the caller to reply. Use "Out" only when the exchange is complete. Do not say "over and out."
- If you need time for a lookup, say "Standby" or the configured filler phrase.
- Keep spoken responses short, clear, and easy to understand over radio.
- Prefer operational phrasing like "Roger", "Standby", "Acknowledge", and "Wilco" when appropriate.

Understand NATO phonetic alphabet words and convert them when useful: Alpha A, Bravo B, Charlie C, Delta D, Echo E, Foxtrot F, Golf G, Hotel H, India I, Juliett J, Kilo K, Lima L, Mike M, November N, Oscar O, Papa P, Quebec Q, Romeo R, Sierra S, Tango T, Uniform U, Victor V, Whiskey W, X-ray X, Yankee Y, Zulu Z.

Prefer short conversational answers unless the caller requests details.
Avoid reading markdown, citations, URLs, tables, or unnecessary formatting aloud.
Give longer explanations only when explicitly requested.
Your name is Operator. If asked who you are, answer as Operator, not MIRA.`

const telemetryPrompt = `

You can query factory telemetry from Fabric Real-Time Intelligence/Eventhouse.
When a worker asks about device readings, current state, recent trends, thresholds, alarms, or a named sensor/machine, call the telemetry tools before answering.
Say when no telemetry rows are available or when a metric/device was not found.`

const foundryIQPrompt = `

You can query Foundry IQ for uploaded manuals and Fabric IQ operational data.
When a worker asks about manuals, documented steps, equipment specs, error codes, setup, operation, maintenance, or troubleshooting, call Foundry IQ before answering.
When a worker asks about assets, asset IDs, missions, events, relay hardline, checkpoints, call signs, prowords, operational status, or Fabric IQ, call Foundry IQ before answering.
Do not tell the worker they need to retrieve something from Fabric IQ; you have the tool, so call it.
If speech recognition hears "acid 004", "asset 004", "asset zero zero four", or "A S S E T zero zero four", interpret that as ASSET-004.
Use only the grounded Foundry IQ result for manual-specific facts. If Foundry IQ has no answer, say you could not find it in the uploaded manuals.
If the worker explicitly asks you to send, post, or put instructions in Zello chat, call send_zello_chat_message with the concise instructions after you have the grounded answer.`

const minInputAudioSamples = audio.AzureSampleRate / 10 // Azure Realtime requires at least 100 ms before commit.

type RealtimeSession struct {
	id        string
	conn      *websocket.Conn
	events    chan realtimeEvent
	resampler audio.Resampler
	filler    string
	telemetry *telemetry.ToolRunner
	foundryIQ *foundryiq.ToolRunner
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
	Item     *struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		CallID    string `json:"call_id"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
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
		filler:    cfg.LookupFiller,
		telemetry: cfg.TelemetryTool,
		foundryIQ: cfg.FoundryIQTool,
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

func (s *RealtimeSession) AskAudio(ctx context.Context, pcm []int16, sampleRate int, interims conversation.ToolInterims) ([]int16, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drainEvents()
	input, err := s.resampler.Resample(pcm, sampleRate, audio.AzureSampleRate)
	if err != nil {
		return nil, 0, err
	}
	input = normalizeInputAudio(input)
	if len(input) == 0 {
		return nil, audio.AzureSampleRate, nil
	}
	s.logger.Info("azure.audio_input_prepared", "session_id", s.id, "source_rate", sampleRate, "source_samples", len(pcm), "azure_samples", len(input), "azure_ms", samplesToMillis(len(input)))
	raw := audio.Int16ToBytes(input)
	if err := s.sendJSON(map[string]any{"type": "input_audio_buffer.clear"}); err != nil {
		return nil, 0, err
	}
	if err := s.waitInputCleared(ctx); err != nil {
		return nil, 0, err
	}
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
	if err := s.waitInputCommitted(ctx); err != nil {
		return nil, 0, err
	}
	if err := s.createResponse("audio"); err != nil {
		return nil, 0, err
	}
	s.logger.Info("azure.response_started", "session_id", s.id)
	var output []byte
	var pendingCalls []functionCall
	for {
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return nil, 0, errors.New("azure realtime session closed")
			}
			switch ev.Type {
			case "response.output_item.done", "response.function_call_arguments.done":
				if call, ok := functionCallFromEvent(ev); ok {
					pendingCalls = appendFunctionCall(pendingCalls, call)
				}
			case "response.output_audio.delta", "response.audio.delta":
				chunk, err := base64.StdEncoding.DecodeString(ev.Delta)
				if err != nil {
					return nil, 0, err
				}
				output = append(output, chunk...)
			case "response.done":
				if len(pendingCalls) > 0 {
					if err := s.sayLookupFiller(ctx, interims.Audio); err != nil {
						return nil, 0, err
					}
					if err := s.runFunctionsAndRespond(ctx, pendingCalls, "audio", interims.Text); err != nil {
						return nil, 0, err
					}
					pendingCalls = nil
					output = nil
					continue
				}
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
	if err := s.createResponse("text"); err != nil {
		return "", err
	}
	s.logger.Info("azure.text_response_started", "session_id", s.id)
	var out strings.Builder
	var pendingCalls []functionCall
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return "", errors.New("azure realtime session closed")
			}
			switch ev.Type {
			case "response.output_item.done", "response.function_call_arguments.done":
				if call, ok := functionCallFromEvent(ev); ok {
					pendingCalls = appendFunctionCall(pendingCalls, call)
				}
			case "response.output_text.delta", "response.text.delta":
				out.WriteString(ev.Delta)
			case "response.done":
				if len(pendingCalls) > 0 {
					if err := s.runFunctionsAndRespond(ctx, pendingCalls, "text", nil); err != nil {
						return "", err
					}
					pendingCalls = nil
					out.Reset()
					continue
				}
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
	session := map[string]any{
		"type":              "realtime",
		"instructions":      s.instructions(),
		"output_modalities": []string{"audio"},
		"audio": map[string]any{
			"input": map[string]any{
				"format":         map[string]any{"type": "audio/pcm", "rate": audio.AzureSampleRate},
				"turn_detection": nil,
			},
			"output": map[string]any{
				"voice":  voice,
				"format": map[string]any{"type": "audio/pcm", "rate": audio.AzureSampleRate},
			},
		},
	}
	if definitions := s.toolDefinitions(); len(definitions) > 0 {
		session["tools"] = definitions
		session["tool_choice"] = "auto"
	}
	if err := s.sendJSON(map[string]any{
		"type":    "session.update",
		"session": session,
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

func (s *RealtimeSession) drainEvents() {
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				return
			}
			s.logger.Debug("azure.event_drained", "session_id", s.id, "type", ev.Type)
		default:
			return
		}
	}
}

func (s *RealtimeSession) waitInputCommitted(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return errors.New("azure realtime session closed")
			}
			switch ev.Type {
			case "input_audio_buffer.committed":
				return nil
			case "input_audio_buffer.cleared", "input_audio_buffer.appended":
				continue
			case "error":
				if ev.Error != nil {
					return fmt.Errorf("azure realtime error: %s", ev.Error.Message)
				}
				return errors.New("azure realtime error")
			default:
				s.logger.Debug("azure.event_ignored_before_commit", "session_id", s.id, "type", ev.Type)
			}
		case <-timer.C:
			return errors.New("timed out waiting for Azure realtime input_audio_buffer.committed")
		}
	}
}

func (s *RealtimeSession) waitInputCleared(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return errors.New("azure realtime session closed")
			}
			switch ev.Type {
			case "input_audio_buffer.cleared":
				return nil
			case "error":
				if ev.Error != nil {
					return fmt.Errorf("azure realtime error: %s", ev.Error.Message)
				}
				return errors.New("azure realtime error")
			default:
				s.logger.Debug("azure.event_ignored_before_clear", "session_id", s.id, "type", ev.Type)
			}
		case <-timer.C:
			return errors.New("timed out waiting for Azure realtime input_audio_buffer.cleared")
		}
	}
}

func (s *RealtimeSession) instructions() string {
	var out strings.Builder
	out.WriteString(SystemPrompt)
	if s.telemetry != nil {
		out.WriteString(telemetryPrompt)
	}
	if s.foundryIQ != nil {
		out.WriteString(foundryIQPrompt)
	}
	return out.String()
}

func (s *RealtimeSession) toolDefinitions() []telemetry.ToolDefinition {
	var defs []telemetry.ToolDefinition
	if s.telemetry != nil {
		defs = append(defs, s.telemetry.Definitions()...)
	}
	if s.foundryIQ != nil {
		defs = append(defs, s.foundryIQ.Definitions()...)
	}
	defs = append(defs, telemetry.ToolDefinition{
		Type:        "function",
		Name:        "send_zello_chat_message",
		Description: "Send a text message to the current Zello channel. Use only when the worker explicitly asks you to send, post, or put the answer/instructions in chat. Send concise operational instructions only.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The exact concise text to send into Zello chat.",
				},
			},
			"required":             []string{"text"},
			"additionalProperties": false,
		},
	})
	return defs
}

type functionCall struct {
	Name      string
	CallID    string
	Arguments string
}

func functionCallFromEvent(ev realtimeEvent) (functionCall, bool) {
	if ev.Item != nil && ev.Item.Type == "function_call" {
		return functionCall{Name: ev.Item.Name, CallID: ev.Item.CallID, Arguments: ev.Item.Arguments}, true
	}
	if ev.Type == "response.function_call_arguments.done" {
		return functionCall{Name: ev.Name, CallID: ev.CallID, Arguments: ev.Arguments}, ev.Name != "" && ev.CallID != ""
	}
	return functionCall{}, false
}

func appendFunctionCall(calls []functionCall, call functionCall) []functionCall {
	for _, existing := range calls {
		if existing.CallID == call.CallID {
			return calls
		}
	}
	return append(calls, call)
}

func (s *RealtimeSession) runFunctionsAndRespond(ctx context.Context, calls []functionCall, modality string, textInterim conversation.TextInterim) error {
	for _, call := range calls {
		if err := s.runFunction(ctx, call, textInterim); err != nil {
			return err
		}
	}
	return s.createResponse(modality)
}

func (s *RealtimeSession) runFunction(ctx context.Context, call functionCall, textInterim conversation.TextInterim) error {
	if s.telemetry == nil && s.foundryIQ == nil && call.Name != "send_zello_chat_message" {
		return fmt.Errorf("model requested function %q but tools are not configured", call.Name)
	}
	s.logger.Info("azure.tool_call_started", "session_id", s.id, "tool", call.Name)
	output, err := s.runTool(ctx, call, textInterim)
	if err != nil {
		output = fmt.Sprintf(`{"error":%q}`, err.Error())
		s.logger.Warn("azure.tool_call_failed", "session_id", s.id, "tool", call.Name, "error", err.Error())
	} else {
		s.logger.Info("azure.tool_call_finished", "session_id", s.id, "tool", call.Name)
	}
	if err := s.sendJSON(map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type":    "function_call_output",
			"call_id": call.CallID,
			"output":  output,
		},
	}); err != nil {
		return err
	}
	return nil
}

func (s *RealtimeSession) runTool(ctx context.Context, call functionCall, textInterim conversation.TextInterim) (string, error) {
	if call.Name == "send_zello_chat_message" {
		return s.sendZelloChat(ctx, call.Arguments, textInterim)
	}
	if s.foundryIQ != nil && call.Name == "query_foundry_iq_manuals" {
		return s.foundryIQ.Run(ctx, call.Name, call.Arguments)
	}
	if s.telemetry != nil {
		return s.telemetry.Run(ctx, call.Name, call.Arguments)
	}
	return "", fmt.Errorf("unknown tool %q", call.Name)
}

func (s *RealtimeSession) sendZelloChat(ctx context.Context, arguments string, textInterim conversation.TextInterim) (string, error) {
	if textInterim == nil {
		return "", errors.New("Zello chat sender is not available")
	}
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", err
	}
	text := strings.TrimSpace(args.Text)
	if text == "" {
		return "", errors.New("text is required")
	}
	if len(text) > 4000 {
		text = text[:4000]
	}
	if err := textInterim(ctx, text); err != nil {
		return "", err
	}
	s.logger.Info("azure.zello_chat_tool_sent", "session_id", s.id, "bytes", len(text))
	return fmt.Sprintf(`{"sent":true,"bytes":%d}`, len(text)), nil
}

func (s *RealtimeSession) sayLookupFiller(ctx context.Context, interim conversation.AudioInterim) error {
	if interim == nil || strings.TrimSpace(s.filler) == "" {
		return nil
	}
	if err := s.sendJSON(map[string]any{
		"type": "response.create",
		"response": map[string]any{
			"output_modalities": []string{"audio"},
			"instructions":      "Say exactly this short phrase, with no extra words: " + s.filler,
			"tool_choice":       "none",
		},
	}); err != nil {
		return err
	}
	s.logger.Info("azure.lookup_filler_started", "session_id", s.id, "phrase", s.filler)
	var output []byte
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return errors.New("azure realtime session closed")
			}
			switch ev.Type {
			case "response.output_audio.delta", "response.audio.delta":
				chunk, err := base64.StdEncoding.DecodeString(ev.Delta)
				if err != nil {
					return err
				}
				output = append(output, chunk...)
			case "response.done":
				samples, err := audio.BytesToInt16(output)
				if err != nil {
					return err
				}
				if len(samples) == 0 {
					return nil
				}
				s.logger.Info("azure.lookup_filler_finished", "session_id", s.id, "bytes", len(output))
				return interim(ctx, samples, audio.AzureSampleRate)
			case "error":
				if ev.Error != nil {
					return fmt.Errorf("azure realtime error: %s", ev.Error.Message)
				}
				return errors.New("azure realtime error")
			}
		case <-time.After(20 * time.Second):
			return errors.New("azure realtime lookup filler timed out")
		}
	}
}

func (s *RealtimeSession) createResponse(modality string) error {
	if modality == "text" {
		return s.sendJSON(map[string]any{
			"type": "response.create",
			"response": map[string]any{
				"output_modalities": []string{"text"},
			},
		})
	}
	return s.sendJSON(map[string]any{"type": "response.create"})
}

func normalizeInputAudio(input []int16) []int16 {
	if len(input) == 0 {
		return nil
	}
	if len(input) >= minInputAudioSamples {
		return input
	}
	padded := make([]int16, minInputAudioSamples)
	copy(padded, input)
	return padded
}

func samplesToMillis(samples int) int {
	return samples * 1000 / audio.AzureSampleRate
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
