package values

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestCommitHintsFollowRealProfilePublicationWithoutValues(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	set := func(value string) bool {
		t.Helper()
		changed, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", value) })
		if e != nil {
			t.Fatal(e)
		}
		return changed
	}
	if !set("synthetic-hint-value-before") {
		t.Fatal("initial value not saved")
	}
	first, err := vault.ReadCommitHint(root)
	if err != nil || first == nil || first.Profile != "app" {
		t.Fatal("committed profile has no hint", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, maintenance.ValueChangeHintPath))
	if err != nil || strings.Contains(string(raw), "synthetic-hint-value") {
		t.Fatal("hint contains value", err)
	}
	if set("synthetic-hint-value-before") {
		t.Fatal("same assignment changed")
	}
	same, err := vault.ReadCommitHint(root)
	if err != nil || *same != *first {
		t.Fatal("same value emitted a new revision hint", err)
	}
	if !set("synthetic-hint-value-after") {
		t.Fatal("changed value not saved")
	}
	next, err := vault.ReadCommitHint(root)
	if err != nil || next.ProfileID != first.ProfileID || next.Generation != first.Generation || next.Revision <= first.Revision {
		t.Fatal("hint detached from logical commit", err)
	}
	// A malformed destination prevents the atomic value/hint transaction; it
	// must not report a failed hint while having silently saved the new value.
	path := filepath.Join(root, maintenance.ValueChangeHintPath)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, e := store.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Variable, "URL", "", "must-not-publish") }); e == nil {
		t.Fatal("invalid hint destination accepted")
	}
	state, e := store.Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	value, _, e := state.GetVariable("URL", "")
	if e != nil || value != "synthetic-hint-value-after" {
		t.Fatal("value published without its atomic hint", e)
	}
}

func TestProtectedCommitHintIncludesEpochButNoSecretAndIsAdvisory(t *testing.T) {
	root, store, master, key := vaultSnapshotFixture(t, 1)
	first, err := vault.ReadCommitHint(root)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	if _, err := store.Update(context.Background(), "app", key, func(p *vault.Profile) (bool, error) {
		return p.Set("SECRET_0000", vault.Secret, "", "synthetic-hint-secret-changed", key)
	}); err != nil {
		t.Fatal(err)
	}
	next, err := vault.ReadCommitHint(root)
	if err != nil || next.Revision <= first.Revision {
		t.Fatal("protected commit lost hint", err)
	}
	if _, err := store.RotateProfile(context.Background(), "app", master); err != nil {
		t.Fatal(err)
	}
	rotated, err := vault.ReadCommitHint(root)
	if err != nil || rotated.KeyEpoch != next.KeyEpoch+1 || rotated.Generation != next.Generation {
		t.Fatal("epoch hint missing", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, maintenance.ValueChangeHintPath))
	if err != nil || strings.Contains(string(raw), "synthetic-hint-secret") || strings.Contains(string(raw), "ciphertext") || strings.Contains(string(raw), "session_id") {
		t.Fatal("secret or authority in hint", err)
	}
	if err := os.WriteFile(filepath.Join(root, maintenance.ValueChangeHintPath), []byte(`{"format":1,"value":"untrusted"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if hint, err := vault.ReadCommitHint(root); hint != nil || err == nil {
		t.Fatal("malformed hint trusted")
	}
}
