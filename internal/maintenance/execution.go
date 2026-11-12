package maintenance

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
)

type executionLeaseKey struct{}

type executionLease struct {
	root     string
	released atomic.Bool
}

func executionLock(root string) string {
	return filepath.Join(root, ".execution", "lock")
}

// AcquireExecution admits a launch or lifecycle operation before it writes an
// execution record or request receipt. Synchronous child operations at the same
// data root can use the returned context to borrow admission. The lease is
// separate from maintenance because an admitted operation can read values and
// allocate ports through the ordinary maintenance gate.
func AcquireExecution(ctx context.Context, root string) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if !filepath.IsAbs(root) {
		return ctx, nil, errors.New("execution data root must be absolute")
	}
	root = filepath.Clean(root)
	if lease, ok := ctx.Value(executionLeaseKey{}).(*executionLease); ok && lease.root == root && !lease.released.Load() {
		return ctx, func() {}, nil
	}
	release, err := LockShared(ctx, executionLock(root))
	if err != nil {
		return ctx, nil, err
	}
	lease := &executionLease{root: root}
	return context.WithValue(ctx, executionLeaseKey{}, lease), sync.OnceFunc(func() {
		lease.released.Store(true)
		release()
	}), nil
}

// AcquireExclusiveExecution waits for admitted execution operations to finish
// and prevents new ones from entering until release. It must precede the
// data-root maintenance gate.
func AcquireExclusiveExecution(ctx context.Context, root string) (func(), error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("execution data root must be absolute")
	}
	return LockExclusive(ctx, executionLock(root))
}
