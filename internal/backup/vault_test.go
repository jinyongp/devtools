package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func protectedBackupFixture(t *testing.T) (Engine, vault.FileStore, *vault.Material, *atomic.Int32) {
	t.Helper()
	root := privateTestRoot(t)
	data := filepath.Join(root, "data")
	vid, err := vault.NewID()
	if err != nil {
		t.Fatal(err)
	}
	pass := vault.NewMaterial([]byte("synthetic-backup-root-unlock"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: filepath.Join(data, "vault")}).Initialize(context.Background(), vid, "passphrase", vault.PassphraseProtector{VaultID: vid, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(master.Destroy)
	store := vault.FileStore{Root: data, VaultID: vid, CatalogKey: master}
	if err = store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	engine := Engine{Data: data, Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache"), VaultMaster: func(context.Context) (*vault.Material, error) {
		calls.Add(1)
		b := master.CopyBytes()
		defer clear(b)
		return vault.NewMaterial(b), nil
	}}
	return engine, store, master, &calls
}
func addProtectedBackupProfile(t *testing.T, store vault.FileStore, master *vault.Material, sealed bool) (*vault.Profile, *vault.Material) {
	t.Helper()
	p, err := vault.NewProfile("app")
	if err != nil {
		t.Fatal(err)
	}
	var key *vault.Material
	if sealed {
		key, err = vault.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(key.Destroy)
		p, err = vault.NewSealedProfile("app", store.VaultID, master, key)
		if err != nil {
			t.Fatal(err)
		}
	}
	p.CreateEnv("local", key)
	p.Set("URL", vault.Public, "", "https://example.invalid", key)
	if sealed {
		p.Set("TOKEN", vault.Secret, "", "synthetic-vault-backup-secret", key)
		p.Set("TOKEN", vault.Secret, "local", "synthetic-expired-backup-secret", key)
		expiry := time.Now().Add(-time.Hour).Unix()
		p.Values[1].Slots[1].ExpiresAt = &expiry
	}
	bundle, _ := vault.NewID()
	p.Bundles = []vault.Bundle{{ID: bundle, Name: "api", ValueIDs: []string{p.Values[0].ID}}}
	if err = p.Sign(key); err != nil {
		t.Fatal(err)
	}
	if err = store.Create(context.Background(), p, key); err != nil {
		t.Fatal(err)
	}
	return p, key
}
func backupIdentity(t *testing.T) (string, string) {
	t.Helper()
	root := privateTestRoot(t)
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipient); err != nil {
		t.Fatal(err)
	}
	return identity, recipient
}

func TestProtectedLogicalBackupRoundTripAndEncryptedReplay(t *testing.T) {
	ctx := context.Background()
	source, sourceStore, sourceMaster, sourceCalls := protectedBackupFixture(t)
	original, _ := addProtectedBackupProfile(t, sourceStore, sourceMaster, true)
	identity, recipient := backupIdentity(t)
	archive := filepath.Join(privateTestRoot(t), "portable.age")
	if _, err := source.Create(ctx, "", archive, recipient); err != nil {
		t.Fatal("protected export", err)
	}
	if sourceCalls.Load() != 1 {
		t.Fatal("export acquired native/session keys per slot/profile")
	}
	meta, metadataErr := Inspect(archive, identity)
	if metadataErr != nil || len(meta.Profiles) != 1 {
		t.Fatal("portable archive inspection", metadataErr)
	}
	target, targetStore, targetMaster, targetCalls := protectedBackupFixture(t)
	request := ImportRequest{Path: archive, IdentityPath: identity, Target: "copy"}
	preview, e := target.Import(ctx, request)
	if e != nil {
		t.Fatal("protected preview", e)
	}
	request.Expected = preview.Plan.Digest
	request.RequestID = "00000000-0000-4000-8000-000000000101"
	result, e := target.Import(ctx, request)
	if e != nil || !result.Plan.Applied {
		t.Fatal("protected restore", e)
	}
	copied, key, err := targetStore.ReadWithMaster(ctx, "copy", targetMaster)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	if copied.ID == original.ID || copied.Generation == original.Generation || copied.VaultID == original.VaultID || copied.Values[1].Slots[0].Version != original.Values[1].Slots[0].Version || len(copied.Bundles) != 1 {
		t.Fatal("local key/identity or portable logical version incorrect")
	}
	env, _, err := copied.Environment("", key, time.Now())
	if err != nil || env["TOKEN"] != "synthetic-vault-backup-secret" {
		t.Fatal("restored value", err)
	}
	if _, _, err = copied.Environment("local", key, time.Now()); err == nil || err.Error() != "value_expired" {
		t.Fatal("restore lost expired override", err)
	}
	replay, e := target.Import(ctx, request)
	if e != nil || !replay.Plan.Replayed {
		t.Fatal("protected receipt replay", e)
	}
	if targetCalls.Load() != 3 {
		t.Fatal("each operation must acquire root once")
	}
	changed := request
	changed.Target = "other"
	if _, e = target.Import(ctx, changed); e == nil || e.Code != "request_conflict" {
		t.Fatal("same UUID changed goal", e)
	}
	if _, err = os.Stat(filepath.Join(target.Data, ".maintenance", "preview-key")); !os.IsNotExist(err) {
		t.Fatal("new unprotected preview MAC key created")
	}
	receipt, err := os.ReadFile(filepath.Join(target.Data, "backup-receipts", request.RequestID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Format int `json:"protected_material_format"`
	}
	if json.Unmarshal(receipt, &header) != nil || header.Format != 1 {
		t.Fatal("restore receipt not authenticated/encrypted")
	}
	for _, directory := range []string{source.Data, target.Data} {
		if err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(b, []byte("synthetic-vault-backup-secret")) || bytes.Contains(b, []byte("synthetic-expired-backup-secret")) {
				t.Fatal("plaintext backup runtime artifact")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublicRestoreCannotDowngradeExistingSealedProfile(t *testing.T) {
	ctx := context.Background()
	source, sourceStore, sourceMaster, _ := protectedBackupFixture(t)
	addProtectedBackupProfile(t, sourceStore, sourceMaster, false)
	identity, recipient := backupIdentity(t)
	archive := filepath.Join(privateTestRoot(t), "public.age")
	if _, err := source.Create(ctx, "app", archive, recipient); err != nil {
		t.Fatal(err)
	}
	target, targetStore, targetMaster, _ := protectedBackupFixture(t)
	before, _ := addProtectedBackupProfile(t, targetStore, targetMaster, true)
	request := ImportRequest{Path: archive, IdentityPath: identity, Target: "app", Replace: true, RecoveryDirectory: filepath.Join(target.Data, "recovery")}
	preview, e := target.Import(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	request.Expected = preview.Plan.Digest
	request.RequestID = "00000000-0000-4000-8000-000000000102"
	result, e := target.Import(ctx, request)
	if e != nil || !result.Plan.Applied || result.Plan.SafetyBackup == "" {
		t.Fatal("replace with safety archive", e)
	}
	after, key, err := targetStore.ReadWithMaster(ctx, "app", targetMaster)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	if !after.Sealed || after.Generation == before.Generation {
		t.Fatal("restore downgraded sealed history or reused generation")
	}
	env, _, err := after.Environment("", key, time.Now())
	if err != nil || len(env) != 1 || env["URL"] != "https://example.invalid" {
		t.Fatal("public restore values", err)
	}
}

func TestLegacyBackupReceiptReplaysAfterProtectedMigration(t *testing.T) {
	ctx := context.Background()
	root := privateTestRoot(t)
	engine := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	store := values.Store{Directory: filepath.Join(engine.Data, "profiles"), Profile: "app"}
	if _, e := store.Update(ctx, func(s *values.State) (bool, *protocol.Error) {
		return s.Set(values.Secret, "TOKEN", "", "synthetic-legacy-receipt-secret")
	}); e != nil {
		t.Fatal(e)
	}
	identity, recipient := backupIdentity(t)
	archive := filepath.Join(privateTestRoot(t), "legacy.age")
	if _, e := engine.Create(ctx, "app", archive, recipient); e != nil {
		t.Fatal(e)
	}
	request := ImportRequest{Path: archive, IdentityPath: identity, Target: "copy"}
	preview, e := engine.Import(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	request.Expected = preview.Plan.Digest
	request.RequestID = "00000000-0000-4000-8000-000000000103"
	applied, e := engine.Import(ctx, request)
	if e != nil || !applied.Plan.Applied {
		t.Fatal(e)
	}
	vid, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-migration-root-key"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: filepath.Join(engine.Data, "vault")}).Initialize(ctx, vid, "passphrase", vault.PassphraseProtector{VaultID: vid, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	inventory, err := values.InspectLegacyMigration(ctx, engine.Data, master)
	if err != nil || inventory.LegacyBackupReceiptCount != 1 {
		t.Fatal("backup receipt inventory", err)
	}
	operation, _ := vault.NewID()
	if _, err = values.MigrateLegacy(ctx, engine.Data, vid, master, operation, inventory.Fingerprint); err != nil {
		t.Fatal(err)
	}
	engine.VaultMaster = func(context.Context) (*vault.Material, error) {
		b := master.CopyBytes()
		defer clear(b)
		return vault.NewMaterial(b), nil
	}
	replay, e := engine.Import(ctx, request)
	if e != nil || !replay.Plan.Replayed || replay.Plan.Digest != applied.Plan.Digest {
		t.Fatal("legacy receipt lost on migration", e)
	}
	if _, err = os.Stat(filepath.Join(engine.Data, ".maintenance", "preview-key")); !os.IsNotExist(err) {
		t.Fatal("raw legacy preview key retained or regenerated")
	}
	convert := ImportRequest{Path: archive, IdentityPath: identity, Target: "converted"}
	convertPreview, e := engine.Import(ctx, convert)
	if e != nil {
		t.Fatal(e)
	}
	convert.Expected = convertPreview.Plan.Digest
	convert.RequestID = "00000000-0000-4000-8000-000000000104"
	if _, e = engine.Import(ctx, convert); e != nil {
		t.Fatal("legacy logical value conversion", e)
	}
	converted, key, err := (vault.FileStore{Root: engine.Data, VaultID: vid, CatalogKey: master}).ReadWithMaster(ctx, "converted", master)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	actual, _, err := converted.Environment("", key, time.Now())
	if err != nil || actual["TOKEN"] != "synthetic-legacy-receipt-secret" {
		t.Fatal("legacy archive did not become encrypted target state", err)
	}

}

func TestProtectedRestoreDrainsLaunchLeaseAndRejectsStalePreview(t *testing.T) {
	ctx := context.Background()
	source, sourceStore, sourceMaster, _ := protectedBackupFixture(t)
	addProtectedBackupProfile(t, sourceStore, sourceMaster, true)
	identity, recipient := backupIdentity(t)
	archive := filepath.Join(privateTestRoot(t), "source.age")
	if _, e := source.Create(ctx, "app", archive, recipient); e != nil {
		t.Fatal(e)
	}
	target, targetStore, master, _ := protectedBackupFixture(t)
	request := ImportRequest{Path: archive, IdentityPath: identity, Target: "copy"}
	preview, e := target.Import(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	request.Expected = preview.Plan.Digest
	request.RequestID = "00000000-0000-4000-8000-000000000105"
	release, err := maintenance.LockShared(ctx, filepath.Join(target.Data, "vault", "launch.lock"))
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	_, e = target.Import(short, request)
	cancel()
	release()
	if e == nil || (e.Code != "canceled" && e.Code != "lock_pending") {
		t.Fatal("restore ignored a live launch lease", e)
	}
	if _, _, err = targetStore.ReadWithMaster(ctx, "copy", master); err == nil || err.Error() != "profile_not_found" {
		t.Fatal("blocked restore published a profile", err)
	}
	if _, e = target.Import(ctx, request); e != nil {
		t.Fatal("same request cannot retry after a canceled gate wait", e)
	}
	replace := ImportRequest{Path: archive, IdentityPath: identity, Target: "copy", Replace: true, RecoveryDirectory: filepath.Join(target.Data, "recovery")}
	before, e := target.Import(ctx, replace)
	if e != nil {
		t.Fatal(e)
	}
	identical := replace
	identical.Expected = before.Plan.Digest
	identical.RequestID = "00000000-0000-4000-8000-000000000107"
	if _, e = target.Import(ctx, identical); e != nil {
		t.Fatal("equivalent logical restore", e)
	}
	stale := replace
	stale.Expected = before.Plan.Digest
	stale.RequestID = "00000000-0000-4000-8000-000000000108"
	if _, e = target.Import(ctx, stale); e == nil || e.Code != "revision_conflict" {
		t.Fatal("old generation preview revived after identical restore", e)
	}
	before, e = target.Import(ctx, replace)
	if e != nil {
		t.Fatal(e)
	}
	_, key, err := targetStore.ReadWithMaster(ctx, "copy", master)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	if _, err = targetStore.Update(ctx, "copy", key, func(p *vault.Profile) (bool, error) {
		return p.Set("TOKEN", vault.Secret, "", "synthetic-newer-target-value", key)
	}); err != nil {
		t.Fatal(err)
	}
	replace.Expected = before.Plan.Digest
	replace.RequestID = "00000000-0000-4000-8000-000000000106"
	if _, e = target.Import(ctx, replace); e == nil || e.Code != "revision_conflict" {
		t.Fatal("stale protected preview applied", e)
	}
	// Receipt replay remains valid after a newer mutation and profile key rotation;
	// it must not restore old values or require the old whole encryption key.
	if _, err = targetStore.RotateProfile(ctx, "copy", master); err != nil {
		t.Fatal(err)
	}
	if result, e := target.Import(ctx, request); e != nil || !result.Plan.Replayed {
		t.Fatal("receipt replay depends on old profile key", e)
	}
	p, current, err := targetStore.ReadWithMaster(ctx, "copy", master)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Destroy()
	env, _, err := p.Environment("", current, time.Now())
	if err != nil || env["TOKEN"] != "synthetic-newer-target-value" {
		t.Fatal("replay rolled back later state", err)
	}
}

func TestCurrentPublicBackupUsesFreshGenerationWithoutAKeySession(t *testing.T) {
	ctx := context.Background()
	root := privateTestRoot(t)
	calls := 0
	engine := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache"), VaultMaster: func(context.Context) (*vault.Material, error) { calls++; return nil, vault.ErrUnavailable }}
	store := values.ModernStore{Directory: filepath.Join(engine.Data, "profiles"), Profile: "app"}
	if _, e := store.Update(ctx, func(s *values.State) (bool, *protocol.Error) {
		return s.Set(values.Variable, "URL", "", "https://example.invalid")
	}); e != nil {
		t.Fatal(e)
	}
	identity, recipient := backupIdentity(t)
	if _, e := engine.Configure(filepath.Join(root, "safety"), recipient); e != nil {
		t.Fatal(e)
	}
	archive, e := engine.Create(ctx, "app", filepath.Join(root, "public.age"), recipient)
	if e != nil {
		t.Fatal(e)
	}
	preview, e := engine.Restore(ctx, archive.Path, identity, "app", "app", "", "", true)
	if e != nil {
		t.Fatal(e)
	}
	id := "00000000-0000-4000-8000-000000000151"
	applied, e := engine.Restore(ctx, archive.Path, identity, "app", "app", preview.Digest, id, true)
	if e != nil || !applied.Applied {
		t.Fatal("public apply", e)
	}
	if _, e = engine.Restore(ctx, archive.Path, identity, "app", "app", preview.Digest, "00000000-0000-4000-8000-000000000152", true); e == nil || e.Code != "revision_conflict" {
		t.Fatal("identical public restore revived stale preview", e)
	}
	replay, e := engine.Restore(ctx, archive.Path, identity, "app", "app", preview.Digest, id, true)
	if e != nil || !replay.Replayed {
		t.Fatal("public restore replay", e)
	}
	if calls != 0 {
		t.Fatal("public backup invoked session provider")
	}
	if _, err := os.Stat(filepath.Join(engine.Data, "vault", "key-slot.json")); !os.IsNotExist(err) {
		t.Fatal("public backup initialized protection")
	}
}
