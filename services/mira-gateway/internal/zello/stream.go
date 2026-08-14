package zello

import (
	"time"

	"github.com/dcasati/mira/internal/conversation"
)

type rxStream struct {
	id         uint32
	channel    string
	speaker    string
	sampleRate int
	startedAt  time.Time
	decoder    Decoder
	pcm        []int16
	maxSamples int
	overLimit  bool
}

func (s *rxStream) append(packet []byte) error {
	if s.overLimit {
		return nil
	}
	pcm, err := s.decoder.Decode(packet)
	if err != nil {
		return err
	}
	if len(s.pcm)+len(pcm) > s.maxSamples {
		remaining := s.maxSamples - len(s.pcm)
		if remaining > 0 {
			s.pcm = append(s.pcm, pcm[:remaining]...)
		}
		s.overLimit = true
		return nil
	}
	s.pcm = append(s.pcm, pcm...)
	return nil
}

func (s *rxStream) finish() conversation.Transmission {
	_ = s.decoder.Close()
	return conversation.Transmission{
		Channel:    s.channel,
		Speaker:    s.speaker,
		StreamID:   s.id,
		PCM:        s.pcm,
		SampleRate: s.sampleRate,
		StartedAt:  s.startedAt,
		EndedAt:    time.Now(),
	}
}
