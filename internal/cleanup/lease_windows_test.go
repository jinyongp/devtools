package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestWindowsRetainedEmptyProcessLeaseIsNotCleanupCandidate(t *testing.T) {
	root := privateTestRoot(t)
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")}
	dir := filepath.Join(e.Data, "process-leases")
	if err := fixtureMkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, tasks.ID()+".lock")
	if err := fixtureWriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	plan, err := e.Preview(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range plan.Items {
		if item.Source == path {
			t.Fatal("retained empty lease selected for GC")
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("retained lease removed", err)
	}
}
