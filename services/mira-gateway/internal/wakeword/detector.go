package wakeword

import (
	"context"
	"regexp"
	"strings"
)

type Result struct {
	Activated  bool
	Transcript string
	Query      string
}

type Detector interface {
	Detect(ctx context.Context, pcm []int16, sampleRate int) (Result, error)
	Ready() bool
	Close() error
}

func MatchTranscript(wakeWord, transcript string) Result {
	clean := strings.Join(strings.Fields(transcript), " ")
	if clean == "" {
		return Result{Transcript: transcript}
	}
	loc := []int(nil)
	for _, alias := range WakeAliases(wakeWord) {
		pattern := regexp.MustCompile(`(?i)(^|[^[:alnum:]])` + regexp.QuoteMeta(alias) + `([^[:alnum:]]|$)`)
		match := pattern.FindStringIndex(clean)
		if match != nil && (loc == nil || match[0] < loc[0]) {
			loc = match
		}
	}
	if loc == nil {
		return Result{Transcript: clean}
	}
	query := strings.TrimSpace(clean[loc[1]:])
	query = strings.TrimLeft(query, " ,.:;!?-")
	return Result{Activated: true, Transcript: clean, Query: query}
}

func WakeAliases(wakeWord string) []string {
	seen := map[string]bool{}
	var aliases []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if seen[key] {
			return
		}
		seen[key] = true
		aliases = append(aliases, value)
	}
	for _, part := range strings.FieldsFunc(wakeWord, func(r rune) bool {
		return r == ',' || r == '|' || r == ';'
	}) {
		add(part)
	}
	if seen["mira"] {
		add("Meera")
		add("Myra")
		add("Mirah")
		add("Meara")
	}
	return aliases
}
