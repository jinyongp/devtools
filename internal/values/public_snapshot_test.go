package values

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestPublicSnapshotPinsGenerationAndActualValuesWithoutSession(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	if _, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "https://before.invalid") }); e != nil {
		t.Fatal(e)
	}
	snapshot, e := CapturePublic(ctx, root, "app", "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Destroy()
	metadata := snapshot.Metadata()
	if metadata.IntegrityVerified || metadata.Public == nil || metadata.Launch != nil || metadata.Identity.ProfileID == "" {
		t.Fatal("public-only classification")
	}
	// Metadata copies cannot change authority on the owned snapshot.
	copy := snapshot.Metadata()
	copy.Public.Profile = "other"
	if snapshot.Metadata().Public.Profile != "app" {
		t.Fatal("ticket metadata aliased")
	}
	if _, e = store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "https://after.invalid") }); e != nil {
		t.Fatal(e)
	}
	id, _ := vault.NewID()
	admission, err := vault.BeginPublicLaunch(ctx, filepath.Join(root, "vault"), id, *metadata.Public)
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	finish, err := vault.AcquireLaunchMutation(short, filepath.Join(root, "vault"))
	cancel()
	if finish != nil {
		finish()
	}
	if !errors.Is(err, vault.ErrLockPending) {
		t.Fatal("mutation bypassed actual launch lease", err)
	}
	if err = admission.Finish(ctx, "spawned"); err != nil {
		t.Fatal(err)
	}
	env, e := snapshot.StateCopy().Environment("")
	if e != nil || env["URL"] != "https://before.invalid" {
		t.Fatal("frozen public input changed", e)
	}
	// Replacing with logically equivalent values is still a new incarnation.
	release, err := vault.AcquireLaunchMutation(ctx, filepath.Join(root, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		release()
		t.Fatal(err)
	}
	p, _, err := store.readPublicHeld()
	if err != nil {
		gate()
		release()
		t.Fatal(err)
	}
	logical, err := p.ExportLogical(nil)
	if err != nil {
		gate()
		release()
		t.Fatal(err)
	}
	replacement, key, err := vault.ImportLogical(logical, "app", "", nil)
	logical.Destroy()
	key.Destroy()
	if err == nil {
		var raw []byte
		raw, err = replacement.Encode()
		if err == nil {
			var files map[string][]byte
			files, err = profilekey.Replacement(store.Directory, "app", "values", raw)
			if err == nil {
				err = maintenance.Replace(root, files)
			}
		}
	}
	gate()
	release()
	if err != nil {
		t.Fatal(err)
	}
	id, _ = vault.NewID()
	admission, err = vault.BeginPublicLaunch(ctx, filepath.Join(root, "vault"), id, *metadata.Public)
	if admission != nil {
		admission.Finish(ctx, "canceled")
	}
	if !errors.Is(err, vault.ErrInputChanged) {
		t.Fatal("old public generation admitted", err)
	}
}

func TestAbsentPublicSnapshotsHaveStableRevisionAndRejectNewStorage(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	first, e := CapturePublic(ctx, root, "app", "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	defer first.Destroy()
	second, e := CapturePublic(ctx, root, "app", "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	defer second.Destroy()
	if first.Metadata().Identity.Revision != second.Metadata().Identity.Revision {
		t.Fatal("absent input revision changed")
	}
	if _, e := (ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}).Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "new") }); e != nil {
		t.Fatal(e)
	}
	id, _ := vault.NewID()
	admission, err := vault.BeginPublicLaunch(ctx, filepath.Join(root, "vault"), id, *first.Metadata().Public)
	if admission != nil {
		admission.Finish(ctx, "canceled")
	}
	if !errors.Is(err, vault.ErrInputChanged) {
		t.Fatal("absent snapshot accepted newly created storage", err)
	}
}

func TestProtectedRootEmptyPublicProfileKeepsStableIdentity(t *testing.T) {
	ctx := context.Background()
	root, _, master, _ := vaultSnapshotFixture(t, 0)
	provider := func(context.Context) (*vault.Material, error) {
		raw := master.CopyBytes()
		defer clear(raw)
		return vault.NewMaterial(raw), nil
	}
	first, e := CaptureVault(ctx, root, "new-public", "", true, time.Now(), provider)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Destroy()
	second, e := CaptureVault(ctx, root, "new-public", "", true, time.Now(), provider)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Destroy()
	if first.Metadata().IntegrityVerified || first.Metadata().KeyEpoch != 0 || first.Metadata().Identity != second.Metadata().Identity {
		t.Fatal("empty public identity changed or claimed verification")
	}
}
