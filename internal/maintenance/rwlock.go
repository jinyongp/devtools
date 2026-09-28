package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type lockMode uint8

const (
	lockShared lockMode = iota
	lockExclusive
)

func ensurePrivateLockDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private lock directory required")
	}
	return nil
}

func openPrivateLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		_ = file.Close()
		return nil, errors.New("private lock required")
	}
	return file, nil
}

func flockMode(mode lockMode) int {
	if mode == lockShared {
		return syscall.LOCK_SH | syscall.LOCK_NB
	}
	return syscall.LOCK_EX | syscall.LOCK_NB
}

func acquireFileLock(ctx context.Context, path string, mode lockMode) (*os.File, error) {
	file, err := openPrivateLock(path)
	if err != nil {
		return nil, err
	}
	for {
		if ctx.Err() != nil {
			_ = file.Close()
			return nil, ctx.Err()
		}
		err = syscall.Flock(int(file.Fd()), flockMode(mode))
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func unlockFile(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

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
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
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
