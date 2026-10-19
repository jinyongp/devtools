package values

import (
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestLogicalBackupCompatibilityKeepsOriginalTTLAndVersions(t *testing.T) {
	master, _ := vault.NewKey()
	defer master.Destroy()
	key, _ := vault.NewKey()
	defer key.Destroy()
	vid, _ := vault.NewID()
	p, err := vault.NewSealedProfile("source", vid, master, key)
	if err != nil {
		t.Fatal(err)
	}
	p.CreateEnv("local", key)
	p.Set("TOKEN", vault.Secret, "", "synthetic-logical-common", key)
	p.Set("TOKEN", vault.Secret, "local", "synthetic-logical-expired", key)
	expiry := time.Now().Add(-time.Hour).Unix()
	p.Values[0].Slots[1].ExpiresAt = &expiry
	bundleID, _ := vault.NewID()
	p.Bundles = []vault.Bundle{{ID: bundleID, Name: "api", ValueIDs: []string{p.Values[0].ID}}}
	if err = p.Sign(key); err != nil {
		t.Fatal(err)
	}
	material, err := p.ExportLogical(key)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	raw := material.CopyBytes()
	defer clear(raw)
	state, err := InspectSnapshot(raw, "source")
	if err != nil {
		t.Fatal(err)
	}
	inventory := state.ScopeInventory()
	if len(inventory) != 1 || inventory[0].Key != "TOKEN" || !inventory[0].Common || len(inventory[0].Envs) != 1 {
		t.Fatal("logical backup metadata changed")
	}
	renamed, err := RestoreSnapshot(raw, "source", "copy")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(renamed)
	if !IsLogicalSnapshot(renamed) {
		t.Fatal("logical archive downgraded to plaintext legacy disk format")
	}
	candidate := vault.NewMaterial(renamed)
	defer candidate.Destroy()
	q, qkey, err := vault.ImportLogical(candidate, "copy", vid, master)
	if err != nil {
		t.Fatal(err)
	}
	defer qkey.Destroy()
	if q.Values[0].Slots[0].Version != p.Values[0].Slots[0].Version || len(q.Bundles) != 1 || q.Bundles[0].ID != bundleID {
		t.Fatal("metadata view altered original transfer")
	}
	if _, _, err = q.Environment("local", qkey, time.Now()); err == nil || err.Error() != "value_expired" {
		t.Fatal("compatibility path lost expiry", err)
	}
}
