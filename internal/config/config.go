package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	WakeWord            string
	ConversationTimeout time.Duration
	WhisperModelPath    string
	MaxRXSeconds        int
	MaxTXSeconds        int
	AudioFile           string

	ZelloEndpoint  string
	ZelloUsername  string
	ZelloPassword  string
	ZelloChannel   string
	ZelloAuthToken string

	AzureOpenAIEndpoint           string
	AzureOpenAIRealtimeDeployment string
	AzureVoice                    string
	LookupFiller                  string

	FabricKQLEndpoint        string
	FabricKQLDatabase        string
	FabricKQLTable           string
	FabricKQLTokenScope      string
	TelemetryTimeColumn      string
	TelemetryDeviceColumn    string
	TelemetryDefaultLookback time.Duration
	TelemetryMaxRows         int

	FoundryProjectEndpoint    string
	FoundryIQAgentName        string
	FoundryIQModel            string
	FoundryIQMaxOutputChars   int
	FoundryIQSearchEndpoint   string
	FoundryIQSearchAPIKey     string
	FoundryIQFabricKB         string
	FoundryIQQuerySourceToken string

	HTTPListenAddr string
	LogLevel       slog.Level
}

func Load() (Config, error) {
	cfg := Config{
		WakeWord:                      env("MIRA_WAKE_WORD", "MIRA,OPERATOR"),
		ConversationTimeout:           durationEnv("MIRA_CONVERSATION_TIMEOUT", 45*time.Second),
		WhisperModelPath:              os.Getenv("MIRA_WHISPER_MODEL_PATH"),
		MaxRXSeconds:                  intEnv("MIRA_MAX_RX_SECONDS", 60),
		MaxTXSeconds:                  intEnv("MIRA_MAX_TX_SECONDS", 60),
		AudioFile:                     os.Getenv("MIRA_AUDIO_FILE"),
		ZelloEndpoint:                 env("ZELLO_ENDPOINT", "wss://zello.io/ws"),
		ZelloUsername:                 os.Getenv("ZELLO_USERNAME"),
		ZelloPassword:                 os.Getenv("ZELLO_PASSWORD"),
		ZelloChannel:                  os.Getenv("ZELLO_CHANNEL"),
		ZelloAuthToken:                os.Getenv("ZELLO_AUTH_TOKEN"),
		AzureOpenAIEndpoint:           os.Getenv("AZURE_OPENAI_ENDPOINT"),
		AzureOpenAIRealtimeDeployment: firstNonEmpty(os.Getenv("AZURE_OPENAI_REALTIME_DEPLOYMENT"), os.Getenv("AZURE_OPENAI_DEPLOYMENT_NAME")),
		AzureVoice:                    env("AZURE_OPENAI_REALTIME_VOICE", "alloy"),
		LookupFiller:                  env("MIRA_LOOKUP_FILLER", "Hold on."),
		FabricKQLEndpoint:             os.Getenv("FABRIC_KQL_ENDPOINT"),
		FabricKQLDatabase:             os.Getenv("FABRIC_KQL_DATABASE"),
		FabricKQLTable:                os.Getenv("FABRIC_KQL_TABLE"),
		FabricKQLTokenScope:           env("FABRIC_KQL_TOKEN_SCOPE", "https://kusto.kusto.windows.net/.default"),
		TelemetryTimeColumn:           env("MIRA_TELEMETRY_TIME_COLUMN", "Timestamp"),
		TelemetryDeviceColumn:         env("MIRA_TELEMETRY_DEVICE_COLUMN", "DeviceId"),
		TelemetryDefaultLookback:      durationEnv("MIRA_TELEMETRY_DEFAULT_LOOKBACK", 15*time.Minute),
		TelemetryMaxRows:              intEnv("MIRA_TELEMETRY_MAX_ROWS", 20),
		FoundryProjectEndpoint:        os.Getenv("FOUNDRY_PROJECT_ENDPOINT"),
		FoundryIQAgentName:            os.Getenv("FOUNDRY_IQ_AGENT_NAME"),
		FoundryIQModel:                os.Getenv("FOUNDRY_IQ_MODEL"),
		FoundryIQMaxOutputChars:       intEnv("FOUNDRY_IQ_MAX_OUTPUT_CHARS", 6000),
		FoundryIQSearchEndpoint:       os.Getenv("FOUNDRY_IQ_SEARCH_ENDPOINT"),
		FoundryIQSearchAPIKey:         os.Getenv("FOUNDRY_IQ_SEARCH_API_KEY"),
		FoundryIQFabricKB:             env("FOUNDRY_IQ_FABRIC_KB", "ks-fabriciq-operator-matrix"),
		FoundryIQQuerySourceToken:     os.Getenv("FOUNDRY_IQ_QUERY_SOURCE_TOKEN"),
		HTTPListenAddr:                env("HTTP_LISTEN_ADDR", ":8080"),
		LogLevel:                      parseLogLevel(env("LOG_LEVEL", "INFO")),
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	var errs []error
	if c.AudioFile != "" {
		if strings.TrimSpace(c.WhisperModelPath) == "" {
			return errors.New("MIRA_WHISPER_MODEL_PATH is required")
		}
		return nil
	}
	required := map[string]string{
		"MIRA_WHISPER_MODEL_PATH":          c.WhisperModelPath,
		"ZELLO_USERNAME":                   c.ZelloUsername,
		"ZELLO_PASSWORD":                   c.ZelloPassword,
		"ZELLO_CHANNEL":                    c.ZelloChannel,
		"ZELLO_AUTH_TOKEN":                 c.ZelloAuthToken,
		"AZURE_OPENAI_ENDPOINT":            c.AzureOpenAIEndpoint,
		"AZURE_OPENAI_REALTIME_DEPLOYMENT": c.AzureOpenAIRealtimeDeployment,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	if c.ConversationTimeout <= 0 {
		errs = append(errs, errors.New("MIRA_CONVERSATION_TIMEOUT must be positive"))
	}
	if c.MaxRXSeconds <= 0 {
		errs = append(errs, errors.New("MIRA_MAX_RX_SECONDS must be positive"))
	}
	if c.MaxTXSeconds <= 0 {
		errs = append(errs, errors.New("MIRA_MAX_TX_SECONDS must be positive"))
	}
	if !strings.HasPrefix(c.ZelloEndpoint, "wss://") {
		errs = append(errs, errors.New("ZELLO_ENDPOINT must use wss://"))
	}
	if c.TelemetryEnabled() {
		for name, value := range map[string]string{
			"FABRIC_KQL_ENDPOINT": c.FabricKQLEndpoint,
			"FABRIC_KQL_DATABASE": c.FabricKQLDatabase,
			"FABRIC_KQL_TABLE":    c.FabricKQLTable,
		} {
			if strings.TrimSpace(value) == "" {
				errs = append(errs, fmt.Errorf("%s is required when any Fabric KQL telemetry setting is provided", name))
			}
		}
		if c.TelemetryDefaultLookback <= 0 {
			errs = append(errs, errors.New("MIRA_TELEMETRY_DEFAULT_LOOKBACK must be positive"))
		}
		if c.TelemetryMaxRows <= 0 {
			errs = append(errs, errors.New("MIRA_TELEMETRY_MAX_ROWS must be positive"))
		}
	}
	if c.FoundryIQEnabled() {
		for name, value := range map[string]string{
			"FOUNDRY_PROJECT_ENDPOINT": c.FoundryProjectEndpoint,
			"FOUNDRY_IQ_AGENT_NAME":    c.FoundryIQAgentName,
		} {
			if strings.TrimSpace(value) == "" {
				errs = append(errs, fmt.Errorf("%s is required when any Foundry IQ setting is provided", name))
			}
		}
		if c.FoundryProjectEndpoint != "" && !strings.HasPrefix(c.FoundryProjectEndpoint, "https://") {
			errs = append(errs, errors.New("FOUNDRY_PROJECT_ENDPOINT must use https://"))
		}
		if c.FoundryIQMaxOutputChars <= 0 {
			errs = append(errs, errors.New("FOUNDRY_IQ_MAX_OUTPUT_CHARS must be positive"))
		}
	}
	return errors.Join(errs...)
}

func (c Config) TelemetryEnabled() bool {
	return strings.TrimSpace(c.FabricKQLEndpoint) != "" ||
		strings.TrimSpace(c.FabricKQLDatabase) != "" ||
		strings.TrimSpace(c.FabricKQLTable) != ""
}

func (c Config) FoundryIQEnabled() bool {
	return strings.TrimSpace(c.FoundryProjectEndpoint) != "" ||
		strings.TrimSpace(c.FoundryIQAgentName) != "" ||
		strings.TrimSpace(c.FoundryIQModel) != ""
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
