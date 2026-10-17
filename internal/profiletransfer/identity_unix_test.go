//go:build !windows

package profiletransfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRejectsUnsafeIdentityAndSymlink(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(t *testing.T, store Store)
	}{
		{
			name: "permissive",
			make: func(t *testing.T, store Store) {
				if err := os.Mkdir(store.directory(), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(store.identityPath(), []byte("invalid\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			make: func(t *testing.T, store Store) {
				if err := os.Mkdir(store.directory(), 0700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(store.Data, "target")
				if err := os.WriteFile(target, []byte("invalid\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, store.identityPath()); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := Store{Data: privateRoot(t)}
			test.make(t, store)
			if _, err := store.Prepare(context.Background()); err != ErrInvalidIdentity {
				t.Fatalf("unsafe identity error = %v", err)
			}
		})
	}
}

func TestPrepareCleansPublishedPendingIdentity(t *testing.T) {
	store := Store{Data: privateRoot(t)}
	first, err := store.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pending := store.pendingIdentityPath()
	if err := os.Link(store.identityPath(), pending); err != nil {
		t.Fatal(err)
	}

	reused, err := store.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reused.Changed || reused.Recipient != first.Recipient {
		t.Fatalf("published identity was not reused: %#v %#v", first, reused)
	}
	if _, err := os.Lstat(pending); !os.IsNotExist(err) {
		t.Fatalf("published pending identity was not cleaned: %v", err)
	}
}
