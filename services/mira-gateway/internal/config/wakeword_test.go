package config

import "testing"

func TestWakeWordDefaultIncludesDispatcherAndOperator(t *testing.T) {
	t.Setenv("MIRA_AUDIO_FILE", "fixture.wav")
	t.Setenv("MIRA_WHISPER_MODEL_PATH", "fixture")
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"", "MIRA,OPERATOR,DISPATCHER"},
		{"MIRA,MEERA,MYRA,MIRAH,MEARA,OPERATOR,DISPATCHER", "MIRA,MEERA,MYRA,MIRAH,MEARA,OPERATOR,DISPATCHER"},
		{"CUSTOM,OPERATOR", "CUSTOM,OPERATOR"},
	} {
		t.Setenv("MIRA_WAKE_WORD", tc.value)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.WakeWord != tc.want {
			t.Fatalf("WakeWord=%q, want %q", cfg.WakeWord, tc.want)
		}
	}
}
