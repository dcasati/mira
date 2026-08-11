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
