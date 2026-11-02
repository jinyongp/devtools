package maintenance

import (
	"context"
	"errors"
	"os"
)

// QueryPreparationError describes required or failed storage preparation.
// Cause retains the original OS error without exposing a private path.
type QueryPreparationError struct {
	State string
	Cause error
	// Passive means preparation was observed but never attempted.
	Passive bool
}

func (e *QueryPreparationError) Error() string { return "cannot prepare storage for query" }
func (e *QueryPreparationError) Unwrap() error { return e.Cause }

// AcquirePassiveQuery never initializes or recovers storage. Doctor inspection
// and namespace probes use it when a writable Query fallback would be a side
// effect of diagnosis itself.
func AcquirePassiveQuery(ctx context.Context, root string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, err := acquirePassivePair(ctx, root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &QueryPreparationError{State: "initialization_required", Cause: err, Passive: true}
		}
		return nil, err
	}
	needed, err := recoveryNeeded(root)
	if err != nil {
		release()
		return nil, err
	}
	if needed {
		release()
		return nil, &QueryPreparationError{State: "recovery_required", Cause: errors.New("maintenance recovery required"), Passive: true}
	}
	return release, nil
}

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
