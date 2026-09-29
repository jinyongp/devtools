package profiletransfer

import (
	"context"

	"filippo.io/age"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func privateRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPrepareCreatesAndReusesPrivateIdentity(t *testing.T) {
	store := Store{Data: privateRoot(t)}
	first, err := store.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || first.Recipient == "" {
		t.Fatalf("first prepare: %#v", first)
	}
	second, err := store.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || second.Recipient != first.Recipient {
		t.Fatalf("second prepare: %#v", second)
	}
	directoryInfo, err := os.Lstat(store.directory())
	if err != nil || directoryInfo.Mode().Perm() != 0700 {
		t.Fatalf("directory mode: %v %v", directoryInfo, err)
	}
	identityInfo, err := os.Lstat(store.identityPath())
	if err != nil || identityInfo.Mode().Perm() != 0600 {
		t.Fatalf("identity mode: %v %v", identityInfo, err)
	}
}

func TestPrepareConcurrentCallsConverge(t *testing.T) {
	store := Store{Data: privateRoot(t)}
	start := make(chan struct{})
	type result struct {
		prepared Prepared
		err      error
	}
	results := make(chan result, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			prepared, err := store.Prepare(context.Background())
			results <- result{prepared: prepared, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	recipient := ""
	changed := 0
	for current := range results {
		if current.err != nil {
			t.Fatal(current.err)
		}
		if recipient == "" {
			recipient = current.prepared.Recipient
		}
		if current.prepared.Recipient != recipient {
			t.Fatalf("different recipients: %q != %q", current.prepared.Recipient, recipient)
		}
		if current.prepared.Changed {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("changed count = %d", changed)
	}
}

func TestPrepareRejectsInvalidIdentityWithoutReplacingIt(t *testing.T) {
	store := Store{Data: privateRoot(t)}
	if err := os.Mkdir(store.directory(), 0700); err != nil {
		t.Fatal(err)
	}
	path := store.identityPath()
	invalid := []byte("not-an-age-identity\n")
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prepare(context.Background()); err != ErrInvalidIdentity {
		t.Fatalf("invalid identity error = %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != string(invalid) {
		t.Fatalf("identity changed: %q %v", body, err)
	}
}

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

func TestPrepareRecoversPendingIdentityWithoutGeneratingAnotherKey(t *testing.T) {
	store := Store{Data: privateRoot(t)}
	if err := ensurePrivateDirectory(store.directory()); err != nil {
		t.Fatal(err)
	}
	pendingIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(store.directory(), ".identity-pending")
	if err := os.WriteFile(pending, []byte(pendingIdentity.String()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	prepared, err := store.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Changed || prepared.Recipient != pendingIdentity.Recipient().String() {
		t.Fatalf("pending identity was not recovered: %#v", prepared)
	}
	if _, err := os.Lstat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending identity survived publication: %v", err)
	}
	canonical, err := readIdentity(store.identityPath())
	if err != nil || canonical.String() != pendingIdentity.String() {
		t.Fatalf("recovered identity changed: %v", err)
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

func TestPrepareRejectsAmbiguousPendingIdentity(t *testing.T) {
	store := Store{Data: privateRoot(t)}
	first, err := store.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pending := store.pendingIdentityPath()
	if err := os.WriteFile(pending, []byte(other.String()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Prepare(context.Background()); err != ErrInvalidIdentity {
		t.Fatalf("ambiguous pending identity accepted: %v", err)
	}
	canonical, err := readIdentity(store.identityPath())
	if err != nil || canonical.Recipient().String() != first.Recipient {
		t.Fatalf("canonical identity changed: %v", err)
	}
}
