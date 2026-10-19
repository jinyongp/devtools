package process

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
)

type startedKey struct{}

// WithStarted observes the definitive spawn boundary, before waiting for child
// output or readiness. A launcher uses it to retire a lock admission promptly.
func WithStarted(ctx context.Context, started func() *protocol.Error) context.Context {
	return context.WithValue(ctx, startedKey{}, started)
}
func NotifyStarted(ctx context.Context) *protocol.Error {
	if started, ok := ctx.Value(startedKey{}).(func() *protocol.Error); ok {
		return started()
	}
	return nil
}

// ExecutablePath exposes a frozen absolute path to managed runners, which must
// not resolve argv[0] through a changed PATH again.
func ExecutablePath(ctx context.Context) string { return executablePath(ctx) }
