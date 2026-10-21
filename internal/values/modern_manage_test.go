package values

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestModernManagedPublicReplayAndConflicts(t *testing.T) {
	for _, name := range []string{"app", strings.Repeat("a", 128)} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: name}
			view, e := store.Inspect(ctx, "")
			if e != nil {
				t.Fatal(e)
			}
			again, e := store.Inspect(ctx, "")
			if e != nil || again.Revision != view.Revision {
				t.Fatal("absent revision unstable", e)
			}
			value := "https://example.invalid"
			c := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000111"}
			first, e := store.Apply(ctx, c)
			if e != nil || !first.Changed || first.Replayed {
				t.Fatal("first mutation", e)
			}
			replay, e := store.Apply(ctx, c)
			if e != nil || !replay.Replayed || replay.Revision != first.Revision {
				t.Fatal("replay", e)
			}
			other := "https://other.invalid"
			changed := c
			changed.Value = &other
			if _, e := store.Apply(ctx, changed); e == nil || e.Code != "request_conflict" {
				t.Fatal("UUID accepted changed input", e)
			}
			stale := changed
			stale.RequestID = "00000000-0000-4000-8000-000000000112"
			if _, e := store.Apply(ctx, stale); e == nil || e.Code != "revision_conflict" {
				t.Fatal("stale revision accepted", e)
			}
			noop := c
			noop.Revision = first.Revision
			noop.RequestID = "00000000-0000-4000-8000-000000000113"
			result, e := store.Apply(ctx, noop)
			if e != nil || result.Changed || result.Revision != first.Revision {
				t.Fatal("same-value changed version", e)
			}
			if _, err := os.Stat(filepath.Join(store.root(), "vault", "key-slot.json")); !os.IsNotExist(err) {
				t.Fatal("public edit initialized protection")
			}
		})
	}
}

func TestModernManagedReplaySurvivesRotationAndNewIncarnation(t *testing.T) {
	ctx := context.Background()
	root, files, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "synthetic-managed-receipt-secret"
	c := Change{Action: "secret.set", Key: "TOKEN", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000121"}
	first, e := store.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	before, key, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	logical, err := before.ExportLogical(key)
	key.Destroy()
	if err != nil {
		t.Fatal(err)
	}
	defer logical.Destroy()
	if _, err = files.RotateProfile(ctx, "app", master); err != nil {
		t.Fatal("rotate", err)
	}
	replay, e := store.Apply(ctx, c)
	if e != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatal("rotation lost replay", e)
	}
	current, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	newer := "synthetic-newer-managed-secret"
	next := Change{Action: "secret.set", Key: "TOKEN", Value: &newer, Revision: current.Revision, RequestID: "00000000-0000-4000-8000-000000000122"}
	if _, e = store.Apply(ctx, next); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Apply(ctx, c); e != nil {
		t.Fatal(e)
	}
	verify, k, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	env, _, err := verify.Environment("", k, time.Now())
	k.Destroy()
	if err != nil || env["TOKEN"] != newer {
		t.Fatal("replay rolled back current secret", err)
	}
	finish, err := vault.AcquireLaunchMutation(ctx, filepath.Join(root, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		finish()
		t.Fatal(err)
	}
	replacements, err := files.LogicalReplacementHeld(ctx, "app", logical)
	if err == nil {
		err = maintenance.Replace(root, replacements)
	}
	release()
	finish()
	if err != nil {
		t.Fatal("restore", err)
	}
	restored, k, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	if restored.ID == before.ID || restored.Generation == before.Generation {
		t.Fatal("restore reused local incarnation")
	}
	for _, change := range []Change{c, next} {
		if replay, e = store.Apply(ctx, change); e != nil || !replay.Replayed {
			t.Fatal("replacement lost historical replay", e)
		}
	}
	receipt, err := os.ReadFile(filepath.Join(root, store.receiptRelative(c.RequestID)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(receipt, []byte("fingerprint")) || bytes.Contains(receipt, []byte(value)) {
		t.Fatal("sealed receipt not protected")
	}
}

func TestModernReplayRetiresOnlyExpiredVerificationKeys(t *testing.T) {
	ctx := context.Background()
	root, files, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "synthetic-expiring-receipt-secret"
	c := Change{Action: "secret.set", Key: "TOKEN", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000131"}
	if _, e = store.Apply(ctx, c); e != nil {
		t.Fatal(e)
	}
	old, key, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	if _, err = files.RotateProfile(ctx, "app", master); err != nil {
		t.Fatal(err)
	}
	key, err = files.HistoricalReceiptKeyHeld(ctx, old.ID, old.KeyEpoch)
	if err != nil {
		t.Fatal("live verification key missing", err)
	}
	key.Destroy()
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	relative := store.receiptRelative(c.RequestID)
	id := valueReceiptScope(c.RequestID)
	receipt, _, err := files.ReadValueReceiptHeld(ctx, relative, id)
	if err != nil {
		release()
		t.Fatal(err)
	}
	receipt.CreatedAt = time.Now().Add(-48 * time.Hour).Unix()
	receipt.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	raw, err := json.Marshal(receipt)
	if err != nil {
		release()
		t.Fatal(err)
	}
	plain := vault.NewMaterial(raw)
	clear(raw)
	encrypted, err := vault.SealProtected(ctx, master, files.VaultID, id, vault.MaterialValueReceipt, plain)
	plain.Destroy()
	if err == nil {
		err = maintenance.Replace(root, map[string][]byte{relative: encrypted})
	}
	release()
	if err != nil {
		t.Fatal(err)
	}
	if _, e = store.Apply(ctx, c); e == nil || e.Code != "request_expired" {
		t.Fatal("expired replay was accepted", e)
	}
	view, e = store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	next := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000132"}
	if _, e = store.Apply(ctx, next); e != nil {
		t.Fatal(e)
	}
	key, err = files.HistoricalReceiptKeyHeld(ctx, old.ID, old.KeyEpoch)
	key.Destroy()
	if !errors.Is(err, vault.ErrKeyMissing) {
		t.Fatal("expired verification key retained", err)
	}
	if _, e = store.Apply(ctx, next); e != nil {
		t.Fatal("pruning broke live replay", e)
	}
}

func TestModernReplayPreservesLegacyRequestsThroughMigration(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	legacy := Store{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	if _, e := legacy.Update(ctx, func(s *State) (bool, *protocol.Error) { return s.Set(Secret, "TOKEN", "", "synthetic-legacy-retry") }); e != nil {
		t.Fatal(e)
	}
	view, e := legacy.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "https://example.invalid"
	c := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000141"}
	first, e := legacy.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-migration-replay-passphrase"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Initialize(ctx, id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	inventory, err := InspectLegacyMigration(ctx, root, master)
	if err != nil {
		t.Fatal(err)
	}
	operation, _ := vault.NewID()
	if _, err = MigrateLegacy(ctx, root, id, master, operation, inventory.Fingerprint); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	store := ModernStore{Directory: legacy.Directory, Profile: "app", Master: snapshotMaster(master, &calls)}
	replay, e := store.Apply(ctx, c)
	if e != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatal("legacy receipt replay", e)
	}
}

func TestModernRejectsPublicScopeAfterConcurrentProtectionSetup(t *testing.T) {
	ctx := context.Background()
	root, _, _, _ := vaultSnapshotFixture(t, 0)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "public"}
	// Model a caller which captured the absence of a vault before initialization
	// and only obtained the data gate after protection was published.
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, key, _, err := store.readHeld(ctx, nil, "")
	key.Destroy()
	if !errors.Is(err, vault.ErrInputChanged) {
		t.Fatal("stale unsigned scope accepted protected root", err)
	}
}
