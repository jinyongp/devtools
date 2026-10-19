package values

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestModernPublicValuesNeedNoVaultAndKeepVersions(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	changed, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "https://example.invalid") })
	if e != nil || !changed {
		t.Fatal(e)
	}
	path := filepath.Join(store.Directory, profilekey.CanonicalFilename("app"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := vault.DecodeProfile(before)
	if err != nil || p.Sealed || p.ID == "" || p.Values[0].Slots[0].Version == "" {
		t.Fatal("public logical profile", err)
	}
	changed, e = store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "https://example.invalid") })
	if e != nil || changed {
		t.Fatal("same public value changed", e)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("same public value changed identity/version", err)
	}
	if _, err = os.Stat(filepath.Join(root, "vault", "key-slot.json")); !os.IsNotExist(err) {
		t.Fatal("public variable initialized a vault")
	}
	if _, e = store.Update(ctx, func(s *State) (bool, *protocol.Error) {
		return s.Set(Secret, "TOKEN", "", "synthetic-denied-plaintext-secret")
	}); e == nil || e.Code != "key_missing" {
		t.Fatal("secret write did not require protection", e)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed secret setup changed public profile")
	}
}

func TestModernFirstSecretPromotionAndMetadataNeverInjectsPlaceholders(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	if _, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "https://example.invalid") }); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(store.Directory, profilekey.CanonicalFilename("app"))
	raw, _ := os.ReadFile(path)
	before, _ := vault.DecodeProfile(raw)
	vid, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-modern-unlock"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Initialize(ctx, vid, "passphrase", vault.PassphraseProtector{VaultID: vid, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	inventory, err := InspectLegacyMigration(ctx, root, master)
	if err != nil {
		t.Fatal(err)
	}
	operation, _ := vault.NewID()
	if _, err = MigrateLegacy(ctx, root, vid, master, operation, inventory.Fingerprint); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	store.Master = func(context.Context) (*vault.Material, error) {
		calls.Add(1)
		b := master.CopyBytes()
		defer clear(b)
		return vault.NewMaterial(b), nil
	}
	invoked := 0
	if _, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) {
		invoked++
		return s.Set(Secret, "TOKEN", "", "synthetic-modern-encrypted-secret")
	}); e != nil {
		t.Fatal("first-secret promotion", e)
	}
	if invoked != 1 || calls.Load() != 1 {
		t.Fatal("promotion reran callback or accessed key backend repeatedly")
	}
	p, key, err := (vault.FileStore{Root: root, VaultID: vid, CatalogKey: master}).ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	if !p.Sealed || p.ID != before.ID || p.Generation != before.Generation || p.Values[0].Slots[0].Version != before.Values[0].Slots[0].Version {
		t.Fatal("first secret reset public identities")
	}
	env, _, err := p.Environment("", key, time.Now())
	if err != nil || env["TOKEN"] != "synthetic-modern-encrypted-secret" {
		t.Fatal(err)
	}
	metadata, e := store.Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = metadata.Environment(""); e == nil {
		t.Fatal("metadata placeholders could be injected")
	}
	if _, e = CaptureLegacy(metadata, "", true); e == nil {
		t.Fatal("legacy execution fallback accepted sealed metadata")
	}
	if calls.Load() != 1 {
		t.Fatal("metadata read touched/unlocked the key session")
	}
	if value, _, e := metadata.GetVariable("URL", ""); e != nil || value != "https://example.invalid" {
		t.Fatal("locked public read failed", e)
	}
	if _, e = store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Unset(Secret, "TOKEN", "") }); e != nil {
		t.Fatal(e)
	}
	current, currentKey, err := (vault.FileStore{Root: root, VaultID: vid, CatalogKey: master}).ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	defer currentKey.Destroy()
	if !current.Sealed {
		t.Fatal("last secret removal downgraded profile")
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
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
		if bytes.Contains(b, []byte("synthetic-modern-encrypted-secret")) {
			t.Fatal("secret written to plaintext runtime artifacts")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestModernFirstSecretOnPreviouslyAbsentProfile(t *testing.T) {
	root, _, master, _ := vaultSnapshotFixture(t, 0)
	ctx := context.Background()
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "brand-new", Master: func(context.Context) (*vault.Material, error) {
		b := master.CopyBytes()
		defer clear(b)
		return vault.NewMaterial(b), nil
	}}
	if _, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) {
		return s.Set(Secret, "TOKEN", "", "synthetic-new-profile-secret")
	}); e != nil {
		t.Fatal("new profile promotion", e)
	}
}
