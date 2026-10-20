package values

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestModernMutationDoesNotOpenUnrelatedSecrets(t *testing.T) {
	root, files, master, key := vaultSnapshotFixture(t, 2)
	// A correctly authenticated manifest with a broken slot AEAD distinguishes
	// whole-manifest authentication from opening every unrelated secret.
	p, k, err := files.ReadWithMaster(context.Background(), "app", master)
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	p.Values[1].Slots[0].Ciphertext[0] ^= 1
	if err = p.Sign(key); err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err = files.PublishManagedHeld(context.Background(), p, key); err != nil {
		t.Fatal(err)
	}
	release()
	var calls atomic.Int32
	s := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	if _, e := s.Update(context.Background(), func(e *Editor) (bool, *protocol.Error) { return e.Set(Variable, "URL", "", "public") }); e != nil {
		t.Fatal(e)
	}
	view, e := s.Inspect(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(context.Background(), Change{Action: "bundle.set", Bundle: &BundleChange{Name: "api", Members: []string{"SECRET_0000"}}, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000333"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(context.Background(), func(e *Editor) (bool, *protocol.Error) { return e.Set(Secret, "SECRET_0000", "", "target-only") }); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(context.Background(), func(e *Editor) (bool, *protocol.Error) { return e.Set(Secret, "SECRET_0001", "", "must-fail") }); e == nil || e.Code != vault.ErrIntegrity.Error() {
		t.Fatal("target's AEAD was not checked", e)
	}
}
