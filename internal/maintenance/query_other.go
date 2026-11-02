//go:build !linux && !darwin

package maintenance

import "context"

// AcquireQuery preserves the platform's existing private storage/locking path.
func AcquireQuery(ctx context.Context, root string) (func(), error) {
	return AcquireShared(ctx, root)
}

func LockQueryShared(ctx context.Context, path string) (func(), error) {
	return LockShared(ctx, path)
}

// Retain the platform's established ACL and byte-range locking behavior.
func TryQueryExclusive(ctx context.Context, path string) (func(), bool, error) {
	return TryLockExclusive(ctx, path)
}
