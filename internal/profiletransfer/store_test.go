package profiletransfer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"filippo.io/age"
)

func privateRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(privateTestRoot(t), "data")
	if err := fixtureMkdir(root, 0700); err != nil {
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
	assertIdentityPrivacy(t, store)
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
	if err := fixtureMkdir(store.directory(), 0700); err != nil {
		t.Fatal(err)
	}
	path := store.identityPath()
	invalid := []byte("not-an-age-identity\n")
	if err := fixtureWriteFile(path, invalid, 0600); err != nil {
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
	if err := fixtureWriteFile(pending, []byte(pendingIdentity.String()+"\n"), 0600); err != nil {
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
	if err := fixtureWriteFile(pending, []byte(other.String()+"\n"), 0600); err != nil {
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
