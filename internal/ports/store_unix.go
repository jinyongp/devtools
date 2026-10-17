//go:build !windows

// Package ports persists local TCP assignments and execution-location identities.
package ports

import (
	"context"

	"errors"

	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) lock(ctx context.Context, name string, wait bool, create bool) (*os.File, *protocol.Error) {
	if create {
		if os.MkdirAll(s.Directory, 0700) != nil {
			return nil, storageError()
		}
	}
	i, e := os.Lstat(s.Directory)
	if errors.Is(e, os.ErrNotExist) && !create {
		return nil, nil
	}
	if e != nil || !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return nil, storageError()
	}
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		flags |= os.O_CREATE
	}
	f, e := os.OpenFile(filepath.Join(s.Directory, name), flags, 0600)
	if errors.Is(e, os.ErrNotExist) && !create {
		return nil, nil
	}
	if e != nil {
		return nil, storageError()
	}
	if !private(f) {
		f.Close()
		return nil, storageError()
	}
	for {
		if ctx.Err() != nil {
			f.Close()
			return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return f, nil
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EINTR) {
			f.Close()
			return nil, storageError()
		}
		if !wait {
			f.Close()
			return nil, fail("port_run_active")
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
}
