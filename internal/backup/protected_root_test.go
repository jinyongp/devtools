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
