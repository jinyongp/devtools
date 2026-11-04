// Package progress carries optional operation progress without owning output.
package progress

import (
	"context"
	"sync"
	"sync/atomic"
)

type Event struct {
	ID        uint64
	Message   string
	Finished  bool
	Suspended bool
}

type Observer func(Event)
type observerKey struct{}

var sequence atomic.Uint64

func WithObserver(ctx context.Context, observer Observer) context.Context {
	return context.WithValue(ctx, observerKey{}, observer)
}

// Begin reports a scoped activity. Finishing it restores the enclosing activity
// in a consumer, including when nested lock waits finish out of order.
func Begin(ctx context.Context, message string) func() {
	return observe(ctx, Event{Message: message})
}

// Suspend gives an input prompt or foreground program exclusive use of the
// terminal. The observer clears feedback synchronously and resumes only after
// every overlapping suspension finishes. Activities may continue in the meantime.
func Suspend(ctx context.Context) func() {
	return observe(ctx, Event{Suspended: true})
}

func observe(ctx context.Context, event Event) func() {
	observer, _ := ctx.Value(observerKey{}).(Observer)
	if observer == nil {
		return func() {}
	}
	event.ID = sequence.Add(1)
	observer(event)
	return sync.OnceFunc(func() {
		event.Finished = true
		observer(event)
	})
}
