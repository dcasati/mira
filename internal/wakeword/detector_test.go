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

func TestMatchTranscriptActivatesCommonASRSpellings(t *testing.T) {
	for _, transcript := range []string{"Meera tell me a joke", "hey Myra, weather tomorrow", "Mirah how long to Banff"} {
		if got := MatchTranscript("MIRA", transcript); !got.Activated {
			t.Fatalf("expected activation for %q", transcript)
		}
	}
}

func TestMatchTranscriptActivatesOperator(t *testing.T) {
	got := MatchTranscript("MIRA,OPERATOR", "Operator, get me an exit.")
	if !got.Activated {
		t.Fatal("expected activation")
	}
	if got.Query != "get me an exit." {
		t.Fatalf("query = %q", got.Query)
	}
}
