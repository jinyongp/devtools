//go:build linux || darwin

package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLogQuerySharesExistingWriterLock(t *testing.T) {
	path := filepath.Join(privateTempDir(t), "output.log")
	if _, err := (boundedLogStore{path: path}).write([]byte("private log fixture")); err != nil {
		t.Fatal(err)
	}
	reader, err := acquireLogLock(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if writer, err := acquireLogLock(ctx, path, true); !errors.Is(err, context.DeadlineExceeded) {
		if writer != nil {
			writer()
		}
		reader()
		t.Fatal("writer crossed snapshot lock", err)
	}
	reader()
	writer, err := acquireLogLock(context.Background(), path, true)
	if err != nil {
		t.Fatal("query retained lock", err)
	}
	writer()
}

func TestLogQueryMissingUnsafeAndCanceledLocks(t *testing.T) {
	for _, kind := range []string{"missing", "symlink", "fifo", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(privateTempDir(t), "output.log")
			lock := path + ".lock"
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(path, lock)
			case "fifo":
				err = syscall.Mkfifo(lock, 0600)
			case "permissions":
				err = os.WriteFile(lock, nil, 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			release, err := acquireLogLock(context.Background(), path, false)
			if kind == "missing" {
				if err == nil {
					release()
					t.Fatal("query initialized a missing lock")
				}
				if _, err := os.Stat(lock); !os.IsNotExist(err) {
					t.Fatal("query created lock", err)
				}
				release, err = acquireLogLock(context.Background(), path, true)
				if err != nil {
					t.Fatal(err)
				}
				release()
				release, err = acquireLogLock(context.Background(), path, false)
				if err != nil {
					t.Fatal(err)
				}
				release()
				return
			}
			if err == nil {
				release()
				t.Fatal("unsafe lock admitted")
			}
		})
	}
	path := filepath.Join(privateTempDir(t), "output.log")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := acquireLogLock(ctx, path, false); !errors.Is(err, context.Canceled) {
		if release != nil {
			release()
		}
		t.Fatal("canceled query entered storage", err)
	}
	if _, err := os.Lstat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled query prepared lock", err)
	}
}
