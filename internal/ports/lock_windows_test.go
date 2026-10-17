package ports

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsRegistryLockWaitAndNonblockingRunLease(t *testing.T) {
	store := Store{Directory: filepath.Join(t.TempDir(), "private")}
	missing, e := store.lock(context.Background(), "missing", false, false)
	if e != nil || missing != nil {
		t.Fatal("read-only lock probe created storage")
	}
	first, e := store.lock(context.Background(), "run.lock", false, true)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	if other, e := store.lock(context.Background(), "run.lock", false, true); e == nil || e.Code != "port_run_active" {
		if other != nil {
			other.Close()
		}
		t.Fatalf("nonblocking contention: %v", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if other, e := store.lock(ctx, "run.lock", true, true); e == nil || e.Code != "canceled" {
		if other != nil {
			other.Close()
		}
		t.Fatalf("wait cancel: %v", e)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	after, e := store.lock(context.Background(), "run.lock", false, false)
	if e != nil || after == nil {
		t.Fatal("closed reference still blocked run lock")
	}
	after.Close()
}
