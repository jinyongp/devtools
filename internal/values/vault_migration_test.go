package values

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestLegacyMigrationPreservesLayersAndRetryReceipts(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	secret := Store{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	canary := "synthetic-migration-value"
	if _, err := secret.Update(ctx, func(s *State) (bool, *protocol.Error) {
		s.CreateEnv("dev")
		s.CreateEnv("empty")
		s.Set(Secret, "TOKEN", "", canary)
		s.Set(Secret, "TOKEN", "dev", "override")
		s.Set(Variable, "URL", "", "https://example.invalid")
		s.Keys["UNSET"] = &entry{Kind: Secret, Envs: map[string]string{}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	view, e := secret.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	changeValue := "after"
	change := Change{Action: "variable.set", Key: "READY", Value: &changeValue, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000091"}
	first, e := secret.Apply(ctx, change)
	if e != nil {
		t.Fatal(e)
	}
	before, e := secret.Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	plain := Store{Directory: secret.Directory, Profile: "public"}
	if _, e := plain.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "ENABLED", "", "true") }); e != nil {
		t.Fatal(e)
	}
	oldMAC, err := maintenance.Read(filepath.Join(root, ".maintenance", "values-key"), 32)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(oldMAC)
	receiptPath := filepath.Join(root, secret.receiptRelative(change.RequestID))
	receiptBefore, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	master, err := vault.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	vaultID, _ := vault.NewID()
	id, _ := vault.NewID()
	inventory, err := InspectLegacyMigration(ctx, root, master)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inventory.Profiles, []string{"app", "public"}) || !reflect.DeepEqual(inventory.SecretProfiles, []string{"app"}) || inventory.LegacyReceiptCount != 1 {
		t.Fatal("incomplete legacy inventory")
	}
	result, err := MigrateLegacy(ctx, root, vaultID, master, id, inventory.Fingerprint)
	if err != nil || !result.Committed {
		t.Fatal("migration", err)
	}
	replay, err := MigrateLegacy(ctx, root, vaultID, master, id, inventory.Fingerprint)
	if err != nil || !replay.Replayed {
		t.Fatal("completion replay", err)
	}
	store := vault.FileStore{Root: root, VaultID: vaultID, CatalogKey: master}
	p, key, err := store.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	for _, env := range []string{"", "dev", "empty"} {
		expected, e := before.Environment(env)
		if e != nil {
			t.Fatal(e)
		}
		actual, _, err := p.Environment(env, key, time.Now())
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatal("environment changed", env, err)
		}
	}
	// The shared old key stays available only inside encrypted verification
	// material. Existing retry receipts are preserved byte-for-byte.
	encrypted, err := maintenance.Read(filepath.Join(root, "vault", "legacy-verification.json"), vault.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := vault.OpenProtected(ctx, master, vaultID, vaultID, vault.MaterialLegacyVerification, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	mac := recovered.CopyBytes()
	recovered.Destroy()
	defer clear(mac)
	if !bytes.Equal(mac, oldMAC) {
		t.Fatal("legacy verification key changed")
	}
	receiptAfter, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(receiptBefore, receiptAfter) {
		t.Fatal("legacy receipt changed", err)
	}
	var receipt changeReceipt
	if json.Unmarshal(receiptAfter, &receipt) != nil || receipt.Fingerprint != signed(mac, change) || receipt.Result.Revision != first.Revision {
		t.Fatal("legacy replay cannot be verified")
	}
	// Public-only profiles do not receive an encrypted keyset or catalog entry.
	b, err := maintenance.Read(filepath.Join(root, "profiles", profilekey.CanonicalFilename("public")), vault.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	public, err := vault.DecodeProfile(b)
	if err != nil || public.Sealed || len(public.WrappedKey) != 0 {
		t.Fatal("public-only profile needs vault", err)
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
		if bytes.Contains(b, []byte(canary)) || bytes.Contains(b, oldMAC) {
			t.Fatal("plaintext migration scratch")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMigrationRejectsStalePreview(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	s := Store{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	s.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Secret, "TOKEN", "", "first") })
	master, _ := vault.NewKey()
	defer master.Destroy()
	vid, _ := vault.NewID()
	id, _ := vault.NewID()
	preview, err := InspectLegacyMigration(ctx, root, master)
	if err != nil {
		t.Fatal(err)
	}
	s.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Secret, "TOKEN", "", "second") })
	if _, err = MigrateLegacy(ctx, root, vid, master, id, preview.Fingerprint); err == nil || !strings.Contains(err.Error(), "revision_conflict") {
		t.Fatal("stale preview admitted", err)
	}
	state, e := s.Read(ctx)
	if e != nil {
		t.Fatal("stale preview modified root", e)
	}
	got, _ := state.Environment("")
	if got["TOKEN"] != "second" {
		t.Fatal("later edit lost")
	}
}
