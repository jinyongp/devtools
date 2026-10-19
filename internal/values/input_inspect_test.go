package values

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestPassivePublicInspectionMatchesDeliveryAndDoesNotMigrate(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	spec := InputSpec{Inject: true}
	empty, e := InspectInput(ctx, root, "app", "", spec)
	if e != nil || empty == nil || len(empty.Slots) != 0 {
		t.Fatal("absent inspection", e)
	}
	absent, e := CapturePublic(ctx, root, "app", "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	absentVersion, err := NewInputVersion(absent.Metadata(), "", spec)
	absent.Destroy()
	if err != nil {
		t.Fatal(err)
	}
	if same, known := SameInput(absentVersion, empty); !same || !known {
		t.Fatal("absent delivery cannot be compared to inspection")
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", profilekey.CanonicalFilename("app"))); !os.IsNotExist(err) {
		t.Fatal("passive inspection published a profile")
	}
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	if _, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "synthetic-public-value") }); e != nil {
		t.Fatal(e)
	}
	before, err := os.ReadFile(filepath.Join(store.Directory, profilekey.CanonicalFilename("app")))
	if err != nil {
		t.Fatal(err)
	}
	current, e := InspectInput(ctx, root, "app", "", spec)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := CapturePublic(ctx, root, "app", "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Destroy()
	delivered, err := NewInputVersion(snapshot.Metadata(), "", spec)
	if err != nil {
		t.Fatal(err)
	}
	if same, known := SameInput(delivered, current); !same || !known || current.IntegrityVerified {
		t.Fatal("public comparison mismatch")
	}
	after, err := os.ReadFile(filepath.Join(store.Directory, profilekey.CanonicalFilename("app")))
	if err != nil || string(after) != string(before) {
		t.Fatal("passive inspection changed values", err)
	}
	if _, err := os.Stat(filepath.Join(root, "vault", "key-slot.json")); !os.IsNotExist(err) {
		t.Fatal("public inspection initialized protection")
	}
	legacyStore := Store{Directory: store.Directory, Profile: "legacy"}
	if _, e := legacyStore.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "OLD", "", "synthetic-original") }); e != nil {
		t.Fatal(e)
	}
	legacyPath := filepath.Join(store.Directory, profilekey.LegacyFilename("legacy"))
	raw, _ := os.ReadFile(legacyPath)
	if _, e := InspectInput(ctx, root, "legacy", "", spec); e == nil {
		t.Fatal("legacy input manufactured stable identity")
	}
	rawAfter, _ := os.ReadFile(legacyPath)
	if string(raw) != string(rawAfter) {
		t.Fatal("inspection migrated legacy data")
	}
}

func TestPassiveProtectedInspectionUsesOnlyCachedKeyAndRespectsLock(t *testing.T) {
	root, store, _, key := vaultSnapshotFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := filepath.Join(root, "vault")
	spec := InputSpec{Inject: true}
	if _, e := InspectInput(ctx, root, "app", "local", spec); e == nil {
		t.Fatal("cold inspection unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Fatal("cold inspection started a worker")
	}
	finished := make(chan error, 1)
	go func() { finished <- vault.ServeKeySession(ctx, dir) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("owned inspection fixture did not stop")
		}
	}()
	var client *vault.SessionClient
	for {
		var err error
		client, err = vault.OpenSessionClient(dir)
		if err == nil {
			if _, err = client.Status(ctx); err == nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("session fixture not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
	defer pass.Destroy()
	if err := client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	before, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		v, e := InspectInput(ctx, root, "app", "local", spec)
		if e != nil || v == nil || !v.IntegrityVerified || len(v.Slots) != 2 {
			t.Fatal("cached authenticated metadata", e)
		}
	}
	after, err := client.Status(ctx)
	if err != nil || before.IdleExpiresAt == nil || after.IdleExpiresAt == nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("inspection extended idle", err)
	}
	expired := time.Now().Add(-time.Minute).Unix()
	if _, err := store.Update(ctx, "app", key, func(p *vault.Profile) (bool, error) {
		if _, err := p.Set("URL", vault.Public, "", "synthetic-public-binding", key); err != nil {
			return false, err
		}
		p.Values[0].Slots[0].ExpiresAt = &expired
		p.Revision++
		return true, p.Sign(key)
	}); err != nil {
		t.Fatal(err)
	}
	if _, e := InspectInput(ctx, root, "app", "local", spec); e == nil || e.Code != "value_expired" {
		t.Fatal("expired secret accepted for injected goal", e)
	}
	if v, e := InspectInput(ctx, root, "app", "local", InputSpec{Bindings: []string{"URL"}}); e != nil || v == nil || len(v.Slots) != 1 {
		t.Fatal("metadata-only read incorrectly used expired secret", e)
	}
	for _, tc := range []struct{ name, code string }{{"MISSING", "key_not_found"}, {"SECRET_0001", "kind_conflict"}} {
		if _, e := InspectInput(ctx, root, "app", "local", InputSpec{Bindings: []string{tc.name}}); e == nil || e.Code != tc.code {
			t.Fatal("invalid binding goal accepted", tc.name, e)
		}
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, e := InspectInput(ctx, root, "app", "local", spec); e == nil {
		t.Fatal("passive inspection reopened a manual lock")
	}
	if empty, e := InspectInput(ctx, root, "app", "local", InputSpec{}); e != nil || empty == nil || len(empty.Slots) != 0 {
		t.Fatal("command without profile input accessed locked protection", e)
	}
}
