//go:build !windows

package services

import (
	"context"
	"errors"

	"os"
	"path/filepath"

	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func acquireLogLock(ctx context.Context, logPath string, exclusive bool) (func(), error) {
	dir := filepath.Dir(logPath)
	if err := privateLogDirectory(dir); err != nil {
		return nil, err
	}
	lockPath := logPath + ".lock"
	file, err := platformfs.OpenNoFollow(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if _, err := privateLogFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	mode := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		mode = syscall.LOCK_EX | syscall.LOCK_NB
	}
	for {
		if ctx.Err() != nil {
			_ = file.Close()
			return nil, ctx.Err()
		}
		err = syscall.Flock(int(file.Fd()), mode)
		if err == nil {
			break
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
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
