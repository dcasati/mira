package config

import (
	"testing"
	"time"
)

func TestConfigValidateRequiresProductionSettings(t *testing.T) {
	err := (Config{ConversationTimeout: 45 * time.Second, MaxRXSeconds: 60, MaxTXSeconds: 60, ZelloEndpoint: "wss://zello.io/ws"}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestConfigValidateDevAudioMode(t *testing.T) {
	cfg := Config{AudioFile: "input.wav", WhisperModelPath: "/models/ggml-tiny.en.bin"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestConfigValidateTelemetryRequiresCompleteSettings(t *testing.T) {
	cfg := Config{
		ConversationTimeout:           45 * time.Second,
		WhisperModelPath:              "/models/ggml-tiny.en.bin",
		MaxRXSeconds:                  60,
		MaxTXSeconds:                  60,
		ZelloEndpoint:                 "wss://zello.io/ws",
		ZelloUsername:                 "mira",
		ZelloPassword:                 "secret",
		ZelloChannel:                  "factory",
		ZelloAuthToken:                "token",
		AzureOpenAIEndpoint:           "https://example.openai.azure.com",
		AzureOpenAIRealtimeDeployment: "gpt-realtime",
		FabricKQLEndpoint:             "https://cluster.kusto.fabric.microsoft.com",
		TelemetryDefaultLookback:      15 * time.Minute,
		TelemetryMaxRows:              20,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing Fabric database/table")
	}
}

func TestConfigValidateFoundryIQRequiresCompleteSettings(t *testing.T) {
	cfg := Config{
		ConversationTimeout:           45 * time.Second,
		WhisperModelPath:              "/models/ggml-tiny.en.bin",
		MaxRXSeconds:                  60,
		MaxTXSeconds:                  60,
		ZelloEndpoint:                 "wss://zello.io/ws",
		ZelloUsername:                 "operator",
		ZelloPassword:                 "secret",
		ZelloChannel:                  "factory",
		ZelloAuthToken:                "token",
		AzureOpenAIEndpoint:           "https://example.openai.azure.com",
		AzureOpenAIRealtimeDeployment: "gpt-realtime",
		FoundryProjectEndpoint:        "https://example.services.ai.azure.com/api/projects/factory",
		FoundryIQMaxOutputChars:       6000,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing Foundry IQ agent name")
	}
}
