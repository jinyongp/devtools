//go:build linux || darwin

package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestProfileQueryLockInitializationAndSafety(t *testing.T) {
	for _, kind := range []string{"absent", "gate_absent", "turnstile_absent", "symlink", "permissions", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(privateTempDir(t), "tasks", "profile.json.lock")
			release, err := LockShared(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			release()
			switch kind {
			case "absent":
				err = os.RemoveAll(filepath.Dir(path))
			case "gate_absent":
				err = os.Remove(path)
			case "turnstile_absent":
				err = os.Remove(path + ".turnstile")
			case "permissions":
				err = os.Chmod(path, 0644)
			default:
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "symlink":
					err = os.Symlink(filepath.Join(filepath.Dir(path), "missing"), path)
				case "fifo":
					err = syscall.Mkfifo(path, 0600)
				case "directory":
					err = os.Mkdir(path, 0700)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			release, err = LockQueryShared(context.Background(), path)
			if kind == "absent" || kind == "gate_absent" || kind == "turnstile_absent" {
				if err != nil {
					t.Fatal(err)
				}
				release()
				return
			}
			if err == nil {
				release()
				t.Fatal("unsafe lock accepted")
			}
			var preparation *QueryPreparationError
			if errors.As(err, &preparation) {
				t.Fatal("unsafe lock attempted initialization")
			}
		})
	}
}

func TestProfileQueryLockWriterIntentAndCancellation(t *testing.T) {
	path := filepath.Join(privateTempDir(t), "profile.lock")
	init, err := LockShared(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	init()
	first, err := LockQueryShared(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if first != nil {
			first()
		}
	}()
	second, err := LockQueryShared(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	second()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		r, err := LockExclusive(ctx, path)
		if err == nil {
			r()
		}
		done <- err
	}()
	waitForWriterIntent(t, path+".turnstile")
	short, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if r, err := LockQueryShared(short, path); !errors.Is(err, context.DeadlineExceeded) {
		if r != nil {
			r()
		}
		t.Fatal("query bypassed writer intent", err)
	}
	first()
	first = nil
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	writer, acquired, err := TryLockExclusive(context.Background(), path)
	if err != nil || !acquired {
		t.Fatal("canceled query retained lock", err)
	}
	writer()
}
