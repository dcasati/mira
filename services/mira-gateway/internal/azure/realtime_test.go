package azure

import (
	"testing"

	"github.com/dcasati/mira/internal/audio"
)

func TestNormalizeInputAudioSkipsEmpty(t *testing.T) {
	if got := normalizeInputAudio(nil); got != nil {
		t.Fatalf("expected nil, got %d samples", len(got))
	}
}

func TestNormalizeInputAudioPadsShortInput(t *testing.T) {
	input := make([]int16, audio.AzureSampleRate/20)
	input[0] = 42
	got := normalizeInputAudio(input)
	if len(got) != minInputAudioSamples {
		t.Fatalf("samples = %d, want %d", len(got), minInputAudioSamples)
	}
	if got[0] != 42 {
		t.Fatalf("first sample = %d, want 42", got[0])
	}
}

func TestNormalizeInputAudioKeepsLongInput(t *testing.T) {
	input := make([]int16, minInputAudioSamples+1)
	got := normalizeInputAudio(input)
	if len(got) != len(input) {
		t.Fatalf("samples = %d, want %d", len(got), len(input))
	}
}

func TestFunctionCallFromOutputItemDone(t *testing.T) {
	ev := realtimeEvent{
		Type: "response.output_item.done",
		Item: &struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			CallID    string `json:"call_id"`
			Arguments string `json:"arguments"`
		}{
			Type:      "function_call",
			Name:      "get_device_telemetry",
			CallID:    "call-1",
			Arguments: `{"device_id":"pump-1"}`,
		},
	}
	call, ok := functionCallFromEvent(ev)
	if !ok {
		t.Fatal("expected function call")
	}
	if call.Name != "get_device_telemetry" || call.CallID != "call-1" || call.Arguments == "" {
		t.Fatalf("call = %#v", call)
	}
}

func TestAppendFunctionCallDeduplicatesByCallID(t *testing.T) {
	calls := appendFunctionCall(nil, functionCall{Name: "a", CallID: "call-1"})
	calls = appendFunctionCall(calls, functionCall{Name: "a", CallID: "call-1"})
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}
