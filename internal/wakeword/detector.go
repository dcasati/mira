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
	pattern := regexp.MustCompile(`(?i)(^|[^[:alnum:]])` + regexp.QuoteMeta(wakeWord) + `([^[:alnum:]]|$)`)
	loc := pattern.FindStringIndex(clean)
	if loc == nil {
		return Result{Transcript: clean}
	}
	query := strings.TrimSpace(clean[loc[1]:])
	query = strings.TrimLeft(query, " ,.:;!?-")
	return Result{Activated: true, Transcript: clean, Query: query}
}
