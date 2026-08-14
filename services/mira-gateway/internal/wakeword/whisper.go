//go:build whisper

package wakeword

import (
	"context"
	"errors"
	"strings"

	whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

type WhisperDetector struct {
	wakeWord string
	model    whisper.Model
}

func NewWhisperDetector(modelPath, wakeWord string) (*WhisperDetector, error) {
	model, err := whisper.New(modelPath)
	if err != nil {
		return nil, err
	}
	return &WhisperDetector{wakeWord: wakeWord, model: model}, nil
}

func (d *WhisperDetector) Detect(ctx context.Context, pcm []int16, sampleRate int) (Result, error) {
	if sampleRate != 16000 {
		return Result{}, errors.New("whisper detector expects 16 kHz PCM")
	}
	floatPCM := make([]float32, len(pcm))
	for i, sample := range pcm {
		floatPCM[i] = float32(sample) / 32768.0
	}
	wctx, err := d.model.NewContext()
	if err != nil {
		return Result{}, err
	}
	if err := wctx.Process(floatPCM, nil, nil, nil); err != nil {
		return Result{}, err
	}
	var b strings.Builder
	for {
		segment, err := wctx.NextSegment()
		if err != nil {
			break
		}
		b.WriteString(segment.Text)
		b.WriteByte(' ')
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		default:
		}
	}
	return MatchTranscript(d.wakeWord, b.String()), nil
}

func (d *WhisperDetector) Ready() bool { return d != nil && d.model != nil }

func (d *WhisperDetector) Close() error {
	if d.model != nil {
		d.model.Close()
	}
	return nil
}
