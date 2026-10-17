package maintenance

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsLockWaitCancellationReleasesHandles(t *testing.T) {
	path := filepath.Join(privateTempDir(t), "lock")
	release, err := LockExclusive(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if unlocked, err := LockShared(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if unlocked != nil {
			unlocked()
		}
		t.Fatalf("wait cancellation: %v", err)
	}
	release()
	after, err := LockShared(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	after()
}
