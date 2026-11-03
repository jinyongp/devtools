package retainedbody

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func TestDoctorPreparesMissingPublicationGuardForCurrentBodies(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	s := Store{Root: root}
	if _, err := s.Put(context.Background(), "archive_payload", []byte("synthetic orphan")); err != nil {
		t.Fatal(err)
	}
	// A body may survive an interrupted publication before any guard exists.
	if _, err := os.Stat(filepath.Join(root, ".retained-bodies.lock")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	engine := storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}
	before, condition := engine.Inspect(context.Background())
	if condition != nil || before.Ready {
		t.Fatal(before, condition)
	}
	after, condition := engine.Fix(context.Background())
	if condition != nil || !after.Ready {
		t.Fatal(after, condition)
	}
	release, err := s.Query(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
