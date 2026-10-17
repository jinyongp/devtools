package maintenance

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"

	"os"
	"path/filepath"
)

// lockedFile retains OS-specific parent guards along with the lock handle.
type lockedFile struct {
	*os.File
	closeGuard func() error
}

func (f *lockedFile) Close() error {
	if f.closeGuard != nil {
		return f.closeGuard()
	}
	return f.File.Close()
}

type lockMode uint8

const (
	lockShared lockMode = iota
	lockExclusive
)

func lockWithTurnstile(ctx context.Context, gatePath string, mode lockMode) (func(), error) {
	if err := ensurePrivateLockDirectory(filepath.Dir(gatePath)); err != nil {
		return nil, err
	}
	turnMode := lockShared
	if mode == lockExclusive {
		turnMode = lockExclusive
	}
	turnstile, err := acquireFileLock(ctx, gatePath+".turnstile", turnMode)
	if err != nil {
		return nil, err
	}
	gate, err := acquireFileLock(ctx, gatePath, mode)
	if err != nil {
		unlockFile(turnstile)
		return nil, err
	}
	unlockFile(turnstile)
	return func() { unlockFile(gate) }, nil
}

// LockShared and LockExclusive provide a private-file RW lock with a sibling
// writer-intent turnstile. Callers must keep global->profile lock ordering.
func LockShared(ctx context.Context, path string) (func(), error) {
	return lockWithTurnstile(ctx, path, lockShared)
}

func LockExclusive(ctx context.Context, path string) (func(), error) {
	return lockWithTurnstile(ctx, path, lockExclusive)
}

func recoveryNeeded(root string) (bool, error) {
	if _, err := os.Lstat(pendingPath(root)); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	parent := transactionsRoot(root)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !platformfs.DirectoryPrivate(parent, info) {
		return false, errors.New("invalid transaction directory")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func acquireMaintenanceGate(ctx context.Context, root string, mode lockMode) (func(), error) {
	lockDir := filepath.Join(root, ".maintenance")
	if err := ensurePrivateLockDirectory(lockDir); err != nil {
		return nil, err
	}
	return lockWithTurnstile(ctx, filepath.Join(lockDir, "lock"), mode)
}

func AcquireExclusive(ctx context.Context, root string) (func(), error) {
	release, err := acquireMaintenanceGate(ctx, root, lockExclusive)
	if err != nil {
		return nil, err
	}
	if err := recoverPending(root); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func AcquireShared(ctx context.Context, root string) (func(), error) {
	for {
		release, err := acquireMaintenanceGate(ctx, root, lockShared)
		if err != nil {
			return nil, err
		}
		needed, checkErr := recoveryNeeded(root)
		if checkErr != nil {
			release()
			return nil, checkErr
		}
		if !needed {
			return release, nil
		}
		release()

		exclusive, err := AcquireExclusive(ctx, root)
		if err != nil {
			return nil, err
		}
		exclusive()
		// Re-enter through the shared path and re-check in case another crashed
		// writer left new recovery state between lock handoffs.
	}
}
