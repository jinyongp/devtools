package values

import (
	"context"
	"github.com/jinyongp/devtools/internal/vault"
	"sync/atomic"
	"testing"
	"time"
)

func TestSelectedVaultSnapshotFreezesMembershipValuesAndOneKeyAccess(t *testing.T) {
	root, store, master, key := vaultSnapshotFixture(t, 3)
	ctx := context.Background()
	if _, err := store.Update(ctx, "app", key, func(p *vault.Profile) (bool, error) { return p.SetBundle("api", []string{"SECRET_0000"}, key) }); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	snapshot, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls), "api")
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Destroy()
	env, e := snapshot.StateCopy().Environment("local")
	if e != nil || len(env) != 1 || env["SECRET_0000"] != "synthetic-slot-value-0000" || len(snapshot.Metadata().Slots) != 1 || snapshot.Metadata().Selection != "api" || calls.Load() != 1 {
		t.Fatal("selected snapshot mismatch", e)
	}
	if _, err := store.Update(ctx, "app", key, func(p *vault.Profile) (bool, error) {
		return p.SetBundle("api", []string{"SECRET_0000", "SECRET_0001"}, key)
	}); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Metadata().Slots) != 1 {
		t.Fatal("old snapshot membership mutated")
	}
	next, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls), "api")
	if e != nil {
		t.Fatal(e)
	}
	defer next.Destroy()
	if len(next.Metadata().Slots) != 2 || next.Metadata().Identity.Revision == snapshot.Metadata().Identity.Revision {
		t.Fatal("new member did not affect subsequent input")
	}
	none, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls), "none")
	if e != nil {
		t.Fatal(e)
	}
	defer none.Destroy()
	selected, e := none.StateCopy().Environment("local")
	if e != nil || len(selected) != 0 || len(none.Metadata().Slots) != 0 || !none.Metadata().IntegrityVerified {
		t.Fatal("none bypassed authentication or injected values", e)
	}
	if _, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls), "missing"); e == nil || e.Code != "bundle_not_found" {
		t.Fatal("missing selection fell back", e)
	}
}

func TestAbsentPublicSelectionIdentityAndMissingBundle(t *testing.T) {
	root := privateTempDir(t)
	ctx := context.Background()
	all, e := CapturePublic(ctx, root, "app", "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	defer all.Destroy()
	none, e := CapturePublic(ctx, root, "app", "", true, time.Now(), "none")
	if e != nil {
		t.Fatal(e)
	}
	defer none.Destroy()
	if all.Metadata().Identity.Revision == none.Metadata().Identity.Revision || none.Metadata().Selection != "none" || none.Metadata().Public == nil {
		t.Fatal("selection identity or public ticket lost")
	}
	if _, e := CapturePublic(ctx, root, "app", "", true, time.Now(), "missing"); e == nil || e.Code != "bundle_not_found" {
		t.Fatal("absent profile silently ignored bundle", e)
	}
}
