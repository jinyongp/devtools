package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
)

func TestNewVaultKeepsRootFormatBarrierAndMissingCatalogFailsClosed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	master, _ := NewKey()
	defer master.Destroy()
	vid, _ := NewID()
	store := FileStore{Root: root, VaultID: vid, CatalogKey: master}
	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireShared(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	got, protected, err := maintenance.ProtectedRootHeld(root)
	release()
	if err != nil || !protected || got != vid {
		t.Fatal("fresh vault has no downgrade barrier", err)
	}
	if err = os.Remove(filepath.Join(root, catalogRelative)); err != nil {
		t.Fatal(err)
	}
	if err = store.Initialize(ctx); !errors.Is(err, ErrKeyMissing) {
		t.Fatal("missing catalog initialized as a new empty vault", err)
	}
}

func TestVaultProfileDiscoveryUsesIdentityWithoutTreatingCatalogAsProfile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	master, _ := NewKey()
	defer master.Destroy()
	vid, _ := NewID()
	key, _ := NewKey()
	defer key.Destroy()
	ctx := context.Background()
	store := FileStore{Root: root, VaultID: vid, CatalogKey: master}
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := NewSealedProfile("app", vid, master, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Create(ctx, p, key); err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireShared(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	names, err := profilekey.Enumerate(filepath.Join(root, "profiles"), "values")
	if err != nil || len(names) != 1 || names[0] != "app" {
		t.Fatal("vault profile not discoverable", err)
	}
}
