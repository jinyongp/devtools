package maintenance

import (
	"context"
	"errors"
)

// QueryPreparationError describes a failed writable initialization or recovery
// fallback. Cause retains the original OS error without exposing a private path.
type QueryPreparationError struct {
	State string
	Cause error
}

func (e *QueryPreparationError) Error() string { return "cannot prepare storage for query" }
func (e *QueryPreparationError) Unwrap() error { return e.Cause }

func acquireQueryFallback(ctx context.Context, root, state string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, err := AcquireShared(ctx, root)
	if err == nil {
		return release, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	return nil, &QueryPreparationError{State: state, Cause: err}
}
