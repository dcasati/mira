//go:build !whisper

package wakeword

import (
	"context"
	"errors"
)

type WhisperDetector struct{}

func NewWhisperDetector(_ string, _ string) (*WhisperDetector, error) {
	return nil, errors.New("whisper support was not compiled; build with -tags whisper")
}

func (d *WhisperDetector) Detect(context.Context, []int16, int) (Result, error) {
	return Result{}, errors.New("whisper support was not compiled")
}

func (d *WhisperDetector) Ready() bool { return false }

func (d *WhisperDetector) Close() error { return nil }
