package conversation

import "context"

type speakerContextKey struct{}

// WithSpeaker attaches the current Zello speaker to ctx. HandleTransmission
// and HandleTextMessage set this before calling into the AzureSession, so
// that downstream components with a durable-identity need (e.g. the
// Foundry IQ client chaining previous_response_id across turns, see
// foundryiq.WithConversationKey) have access to an identity that survives
// mira's own MIRA_CONVERSATION_TIMEOUT expiring and starting a new
// Realtime session -- unlike that Realtime session's own session_id,
// which resets every time.
func WithSpeaker(ctx context.Context, speaker string) context.Context {
	return context.WithValue(ctx, speakerContextKey{}, speaker)
}

// SpeakerFromContext returns the speaker attached by WithSpeaker, or ""
// if none was set (e.g. in tests that call an AzureSession directly).
func SpeakerFromContext(ctx context.Context) string {
	speaker, _ := ctx.Value(speakerContextKey{}).(string)
	return speaker
}
