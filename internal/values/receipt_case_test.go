package values

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestManagedRequestCaseSafePathsAndLegacySpelling(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	legacy := Store{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	store := ModernStore{Directory: legacy.Directory, Profile: legacy.Profile}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	text := "synthetic-original-public-value"
	upper := "ABCDEF00-0000-4000-8000-000000000001"
	lower := strings.ToLower(upper)
	c := Change{Action: "variable.set", Key: "OLD", Value: &text, Revision: view.Revision, RequestID: upper}
	old, e := store.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	oldPath := filepath.Join(root, legacy.receiptRelative(upper))
	before, err := os.ReadFile(filepath.Join(root, store.receiptRelative(upper)))
	if err != nil {
		t.Fatal(err)
	}
	// Public receipts have the same historical JSON/MAC form. Place this one
	// at the pre-s2 ModernStore location without rewriting its bytes.
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	err = maintenance.Replace(root, map[string][]byte{legacy.receiptRelative(upper): before, store.receiptRelative(upper): nil})
	release()
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(store.receiptRelative(upper), store.receiptRelative(lower)) {
		t.Fatal("UUID spellings alias on a case-insensitive filesystem")
	}
	long := store
	long.Profile = strings.Repeat("A", 128)
	if len(filepath.Base(long.receiptRelative(upper))) > 255 {
		t.Fatal("case-safe filename exceeds portable component limit")
	}
	if _, e := store.InspectRequest(ctx, lower); e == nil || e.Code != "request_not_found" {
		t.Fatal("legacy case alias was treated as this request", e)
	}
	if got, e := store.InspectRequest(ctx, upper); e != nil || got.Item.Result == nil || got.Item.Result.Revision != old.Revision {
		t.Fatal("exact legacy spelling not readable", e)
	}
	current, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	next := Change{Action: "variable.set", Key: "NEW", Value: &text, Revision: current.Revision, RequestID: lower}
	first, e := store.Apply(ctx, next)
	if e != nil {
		t.Fatal("distinct UUID spelling could not commit", e)
	}
	after, err := os.ReadFile(oldPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("new case variant overwrote historical receipt", err)
	}
	if got, e := store.Apply(ctx, c); e != nil || !got.Replayed || got.Revision != old.Revision {
		t.Fatal("legacy spelling lost replay", e)
	}
	if got, e := store.Apply(ctx, next); e != nil || !got.Replayed || got.Revision != first.Revision {
		t.Fatal("new spelling lost replay", e)
	}
	if got, e := store.InspectRequest(ctx, lower); e != nil || got.Item.Result == nil || got.Item.Result.Revision != first.Revision {
		t.Fatal("new case variant lookup returned legacy result", e)
	}
}

func TestManagedCaseVariantsCannotSwapProtectedReceipts(t *testing.T) {
	ctx := context.Background()
	root, _, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	text := "synthetic-case-private-value"
	upper := "ABCDEF00-0000-4000-8000-000000000001"
	c := Change{Action: "secret.set", Key: "FIRST", Value: &text, Revision: view.Revision, RequestID: upper}
	first, e := store.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	other := c
	other.Key, other.Revision, other.RequestID = "SECOND", first.Revision, strings.ToLower(upper)
	if _, e := store.Apply(ctx, other); e != nil {
		t.Fatal(e)
	}
	raw, err := os.ReadFile(filepath.Join(root, store.receiptRelative(upper)))
	if err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	err = maintenance.Replace(root, map[string][]byte{store.receiptRelative(other.RequestID): raw})
	release()
	if err != nil {
		t.Fatal(err)
	}
	if _, e := store.Apply(ctx, other); e == nil || e.Code != "integrity_failed" {
		t.Fatal("case-variant receipt swap authenticated", e)
	}
}

func TestManagedProtectedLegacyCaseSpellingRetainsOldScope(t *testing.T) {
	ctx := context.Background()
	root, files, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	text := "synthetic-protected-legacy-case"
	upper := "ABCDEF00-0000-4000-8000-000000000001"
	c := Change{Action: "secret.set", Key: "TOKEN", Value: &text, Revision: view.Revision, RequestID: upper}
	first, e := store.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, found, err := files.ReadValueReceiptHeld(ctx, store.receiptRelative(upper), valueReceiptScope(upper))
	if err != nil || !found {
		release()
		t.Fatal("new receipt unreadable", err)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		release()
		t.Fatal(err)
	}
	plain := vault.NewMaterial(raw)
	clear(raw)
	oldScope := strings.ToLower(strings.ReplaceAll(upper, "-", ""))
	raw, err = vault.SealProtected(ctx, master, files.VaultID, oldScope, vault.MaterialValueReceipt, plain)
	plain.Destroy()
	old := (Store{Directory: store.Directory, Profile: store.Profile}).receiptRelative(upper)
	if err == nil {
		err = maintenance.Replace(root, map[string][]byte{old: raw, store.receiptRelative(upper): nil})
	}
	release()
	if err != nil {
		t.Fatal(err)
	}
	if replay, e := store.Apply(ctx, c); e != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatal("historical protected scope no longer replays", e)
	}
	lower := c
	lower.RequestID, lower.Revision = strings.ToLower(upper), first.Revision
	if result, e := store.Apply(ctx, lower); e != nil || result.Replayed || result.Changed {
		t.Fatal("distinct case request was not an independent no-op", e)
	}
	if replay, e := store.Apply(ctx, c); e != nil || !replay.Replayed {
		t.Fatal("new case receipt overwrote old protected spelling", e)
	}
}
