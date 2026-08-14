package queue

import "context"

type Bounded[T any] struct {
	ch chan T
}

func NewBounded[T any](size int) *Bounded[T] {
	return &Bounded[T]{ch: make(chan T, size)}
}

func (q *Bounded[T]) TryPush(v T) bool {
	select {
	case q.ch <- v:
		return true
	default:
		return false
	}
}

func (q *Bounded[T]) Push(ctx context.Context, v T) error {
	select {
	case q.ch <- v:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *Bounded[T]) C() <-chan T { return q.ch }

func (q *Bounded[T]) Len() int { return len(q.ch) }
