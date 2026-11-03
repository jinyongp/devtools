//go:build !linux && !darwin && !windows

package maintenance

import "context"

func TryQueryExclusive(ctx context.Context, path string) (func(), bool, error) {
	return TryLockExclusive(ctx, path)
}
