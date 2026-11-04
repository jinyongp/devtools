// Package progress carries optional operation progress without owning output.
package progress

import (
	"context"
	"sync"
	"sync/atomic"
)

type Event struct {
	ID       uint64
	Message  string
	Finished bool
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
	observer, _ := ctx.Value(observerKey{}).(Observer)
	if observer == nil {
		return func() {}
	}
	id := sequence.Add(1)
	observer(Event{ID: id, Message: message})
	return sync.OnceFunc(func() { observer(Event{ID: id, Finished: true}) })
}
