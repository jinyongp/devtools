//go:build linux || darwin

package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

// AcquireQuery uses the ordinary maintenance lock objects without opening them
// for writing. Missing locks and pending recovery retain the writable shared
// path's initialization and recovery contract; they never admit a lockless read.
func AcquireQuery(ctx context.Context, root string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, err := acquireQueryGate(ctx, root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return acquireQueryFallback(ctx, root, "initialization_required")
		}
		return nil, err
	}
	needed, err := recoveryNeeded(root)
	if err != nil {
		release()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	if needed {
		release()
		return acquireQueryFallback(ctx, root, "recovery_required")
	}
	return release, nil
}

func acquireQueryGate(ctx context.Context, root string) (func(), error) {
	return acquireQueryPair(ctx, filepath.Join(root, ".maintenance", "lock"))
}

// LockQueryShared shares the writer's profile lock without creating or opening
// existing lock objects for writing. Only missing objects use initialization.
func LockQueryShared(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, err := acquireQueryPair(ctx, path)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return release, err
	}
	release, err = LockShared(ctx, path)
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return release, err
	}
	return nil, &QueryPreparationError{State: "initialization_required", Cause: err}
}

func acquireQueryPair(ctx context.Context, path string) (func(), error) {
	directory := filepath.Dir(path)
	if err := platformfs.CheckDirectory(directory); err != nil {
		return nil, err
	}
	turnstile, err := acquireQueryFileLock(ctx, path+".turnstile")
	if err != nil {
		return nil, err
	}
	gate, err := acquireQueryFileLock(ctx, path)
	unlockFile(turnstile)
	if err != nil {
		return nil, err
	}
	return func() { unlockFile(gate) }, nil
}

func acquireQueryFileLock(ctx context.Context, path string) (*lockedFile, error) {
	file, err := platformfs.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := platformfs.CheckFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return waitForFileLock(ctx, file, lockShared)
}
