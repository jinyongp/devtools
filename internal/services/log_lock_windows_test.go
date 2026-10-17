package services

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsLogLocksPreserveSharedExclusiveAndCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "output.log")
	first, err := acquireLogLock(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := acquireLogLock(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if release, err := acquireLogLock(ctx, path, true); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("exclusive log cancellation: %v", err)
	}
	first()
	second()
	after, err := acquireLogLock(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	after()
}
