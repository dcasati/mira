package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	AzureSampleRate = 24000
	WakeSampleRate  = 16000
	ZelloSampleRate = 16000
	PacketMS        = 20
)

type PCM struct {
	Samples    []int16
	SampleRate int
}

func Duration(samples int, sampleRate int) time.Duration {
	if sampleRate <= 0 {
		return 0
	}
	return time.Duration(samples) * time.Second / time.Duration(sampleRate)
}

func Int16ToBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}

func BytesToInt16(data []byte) ([]int16, error) {
	if len(data)%2 != 0 {
		return nil, errors.New("pcm byte length must be even")
	}
	out := make([]int16, len(data)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return out, nil
}

func Limit(samples []int16, sampleRate, maxSeconds int) ([]int16, error) {
	if maxSeconds <= 0 || sampleRate <= 0 {
		return nil, errors.New("invalid audio limit")
	}
	maxSamples := sampleRate * maxSeconds
	if len(samples) > maxSamples {
		return samples[:maxSamples], fmt.Errorf("audio exceeded %ds limit", maxSeconds)
	}
	return samples, nil
}
