//go:build !linux && !darwin

package maintenance

import "context"

// AcquireQuery preserves the platform's existing private storage/locking path.
func AcquireQuery(ctx context.Context, root string) (func(), error) {
	return AcquireShared(ctx, root)
}
