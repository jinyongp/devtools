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

func TestQueryProbeSharesWriterLockObjects(t *testing.T) {
	path := filepath.Join(queryFixture(t), ".maintenance", "lock")
	for _, acquire := range []func(context.Context, string) (func(), error){LockShared, LockExclusive} {
		writer, err := acquire(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if release, free, err := TryQueryExclusive(context.Background(), path); err != nil || free || release != nil {
			writer()
			t.Fatalf("probe bypassed existing writer/reader: free=%v err=%v", free, err)
		}
		writer()
	}
	probe, free, err := TryQueryExclusive(context.Background(), path)
	if err != nil || !free {
		t.Fatal("free pair unavailable", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if release, err := LockShared(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		probe()
		t.Fatal("ordinary reader bypassed exclusive query probe", err)
	}
	probe()
	writer, free, err := TryLockExclusive(context.Background(), path)
	if err != nil || !free {
		t.Fatal("query retained gate/turnstile", err)
	}
	writer()

	intent, err := acquireFileLock(context.Background(), path+".turnstile", lockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	if release, free, err := TryQueryExclusive(context.Background(), path); err != nil || free || release != nil {
		unlockFile(intent)
		t.Fatal("probe bypassed writer intent", err)
	}
	unlockFile(intent)
}

func TestQueryProbeNeverPreparesOrAdmitsUnsafeStorage(t *testing.T) {
	for _, kind := range []string{"missing_directory", "missing_turnstile", "missing_gate", "symlink", "permissions", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(queryFixture(t), ".maintenance", "lock")
			switch kind {
			case "missing_directory":
				path = filepath.Join(filepath.Dir(path), "absent", "lock")
			case "missing_turnstile":
				if err := os.Remove(path + ".turnstile"); err != nil {
					t.Fatal(err)
				}
			case "missing_gate":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "fifo" {
					err = syscall.Mkfifo(path, 0600)
				} else {
					err = os.Symlink(path+".turnstile", path)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if release, free, err := TryQueryExclusive(context.Background(), path); err == nil || free || release != nil {
				t.Fatal("missing/unsafe probe accepted", err)
			} else if kind == "missing_directory" || kind == "missing_gate" || kind == "missing_turnstile" {
				var preparation *QueryPreparationError
				if !errors.As(err, &preparation) || preparation.State != "initialization_required" {
					t.Fatal("missing preparation state", err)
				}
			}
			if kind == "missing_directory" || kind == "missing_gate" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("probe created lock", err)
				}
			}
			if kind == "missing_turnstile" {
				if _, err := os.Lstat(path + ".turnstile"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("probe created turnstile", err)
				}
			} else if kind != "missing_directory" {
				intent, acquired, err := tryAcquireFileLock(context.Background(), path+".turnstile", lockExclusive)
				if err != nil || !acquired {
					t.Fatal("failed second open retained turnstile", err)
				}
				unlockFile(intent)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, free, err := TryQueryExclusive(ctx, filepath.Join(t.TempDir(), "missing")); !errors.Is(err, context.Canceled) || free || release != nil {
		t.Fatal("canceled probe accessed storage", err)
	}
}
