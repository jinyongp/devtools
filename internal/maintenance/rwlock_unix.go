//go:build !windows

package maintenance

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"

	"syscall"
	"time"
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
	file, err := platformfs.OpenNoFollow(path, os.O_CREATE|os.O_RDWR, 0600)
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
