package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsQueryRetainsExistingMaintenanceGate(t *testing.T) {
	root := privateTempDir(t)
	query, err := AcquireQuery(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := AcquireShared(context.Background(), root)
	if err != nil {
		query()
		t.Fatal(err)
	}
	shared()
	query()
	writer, err := AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer writer()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if release, err := AcquireQuery(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatal("query bypassed Windows writer", err)
	}
}

func TestWindowsExclusiveQueryProbeNeverPreparesAndHonorsWriters(t *testing.T) {
	path := filepath.Join(privateTempDir(t), "probe.lock")
	if release, free, err := TryQueryExclusive(context.Background(), path); err == nil || free || release != nil {
		t.Fatal("query prepared an absent pair", free, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("query created a lock", err)
	}
	writer, err := LockExclusive(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if release, free, err := TryQueryExclusive(context.Background(), path); err != nil || free || release != nil {
		writer()
		t.Fatal("query bypassed writer", free, err)
	}
	writer()
	release, free, err := TryQueryExclusive(context.Background(), path)
	if err != nil || !free {
		t.Fatal("prepared pair unavailable", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if reader, err := LockShared(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if reader != nil {
			reader()
		}
		release()
		t.Fatal("probe did not retain exclusive lifetime", err)
	}
	release()
}
