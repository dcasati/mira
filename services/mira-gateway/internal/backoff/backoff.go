package backoff

import (
	"math/rand/v2"
	"time"
)

type Backoff struct {
	base    time.Duration
	max     time.Duration
	current time.Duration
}

func New(base, max time.Duration) *Backoff {
	return &Backoff{base: base, max: max}
}

func (b *Backoff) Next() time.Duration {
	if b.current == 0 {
		b.current = b.base
	} else {
		b.current *= 2
		if b.current > b.max {
			b.current = b.max
		}
	}
	jitter := time.Duration(rand.Int64N(int64(b.current / 3)))
	return b.current - jitter/2 + jitter
}

func (b *Backoff) Reset() { b.current = 0 }
