package backoff

import (
	"testing"
	"time"
)

func TestBackoffCapsAndResets(t *testing.T) {
	b := New(time.Second, 4*time.Second)
	var last time.Duration
	for i := 0; i < 10; i++ {
		last = b.Next()
		if last > 6*time.Second {
			t.Fatalf("unexpected backoff with jitter: %v", last)
		}
	}
	b.Reset()
	if next := b.Next(); next > 2*time.Second {
		t.Fatalf("reset next = %v", next)
	}
}
