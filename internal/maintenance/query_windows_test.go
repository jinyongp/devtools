package maintenance

import (
	"context"
	"errors"
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
