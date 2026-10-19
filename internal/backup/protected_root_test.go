package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestLegacyRestoreCannotCreatePlaintextProfileInProtectedRoot(t *testing.T) {
	ctx := context.Background()
	root := privateTestRoot(t)
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	target := Engine{Data: filepath.Join(root, "target", "data"), Config: filepath.Join(root, "target", "config"), Cache: filepath.Join(root, "target", "cache")}
	for _, engine := range []Engine{source, target} {
		store := values.Store{Directory: filepath.Join(engine.Data, "profiles"), Profile: "app"}
		if _, err := store.Update(ctx, func(s *values.State) (bool, *protocol.Error) {
			return s.Set(values.Secret, "TOKEN", "", "synthetic-protected-root-canary")
		}); err != nil {
			t.Fatal(err)
		}
	}
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipient); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "source.age")
	if _, err := source.Create(ctx, "app", archive, recipient); err != nil {
		t.Fatal(err)
	}
	master, err := vault.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	vid, _ := vault.NewID()
	id, _ := vault.NewID()
	preview, err := values.InspectLegacyMigration(ctx, target.Data, master)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = values.MigrateLegacy(ctx, target.Data, vid, master, id, preview.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Import(ctx, ImportRequest{Path: archive, IdentityPath: identity, Target: "brand-new"}); err == nil || err.Code != "unsupported_storage" {
		t.Fatal("legacy restore admitted", err)
	}
	if _, err := os.Stat(filepath.Join(target.Data, "profiles", profilekey.CanonicalFilename("brand-new"))); !os.IsNotExist(err) {
		t.Fatal("plaintext profile created")
	}
}

func TestLegacyRestoreRejectsLogicalSecretArchiveOnEmptyRoot(t *testing.T) {
	ctx := context.Background()
	root := privateTestRoot(t)
	identity, public := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, public); err != nil {
		t.Fatal(err)
	}
	master, _ := vault.NewKey()
	defer master.Destroy()
	key, _ := vault.NewKey()
	defer key.Destroy()
	vid, _ := vault.NewID()
	p, err := vault.NewSealedProfile("source", vid, master, key)
	if err != nil {
		t.Fatal(err)
	}
	p.Set("TOKEN", vault.Secret, "", "synthetic-new-logical-archive", key)
	logical, err := p.ExportLogical(key)
	if err != nil {
		t.Fatal(err)
	}
	defer logical.Destroy()
	raw := logical.CopyBytes()
	defer clear(raw)
	recipientValue, err := recipient(public)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := encode(Snapshot{Version: 1, Created: "2026-10-19T00:00:00Z", Profiles: []Profile{{Name: "source", Values: raw}}}, recipientValue)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "logical.age")
	if err = exclusive(archive, encrypted); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	if _, err := engine.Import(ctx, ImportRequest{Path: archive, IdentityPath: identity, Target: "brand-new"}); err == nil || err.Code != "unsupported_storage" {
		t.Fatal("legacy engine accepted logical secret payload", err)
	}
	if _, err = os.Stat(filepath.Join(engine.Data, "profiles", profilekey.CanonicalFilename("brand-new"))); !os.IsNotExist(err) {
		t.Fatal("logical plaintext written as a profile")
	}
}
