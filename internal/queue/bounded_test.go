package queue

import "testing"

func TestBoundedQueueTryPush(t *testing.T) {
	q := NewBounded[int](1)
	if !q.TryPush(1) {
		t.Fatal("first push should succeed")
	}
	if q.TryPush(2) {
		t.Fatal("second push should fail when full")
	}
}
