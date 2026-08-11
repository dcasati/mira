package wakeword

import "testing"

func TestMatchTranscriptActivatesCaseInsensitive(t *testing.T) {
	got := MatchTranscript("MIRA", "Hey mira, tell me a joke.")
	if !got.Activated {
		t.Fatal("expected activation")
	}
	if got.Query != "tell me a joke." {
		t.Fatalf("query = %q", got.Query)
	}
}

func TestMatchTranscriptAvoidsSubstringFalsePositive(t *testing.T) {
	for _, transcript := range []string{"the mirror is dirty", "admiral says hello", "mirage ahead"} {
		if got := MatchTranscript("MIRA", transcript); got.Activated {
			t.Fatalf("unexpected activation for %q", transcript)
		}
	}
}
