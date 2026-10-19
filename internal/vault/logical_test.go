package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"filippo.io/age"
)

func TestLogicalAgeTransferUsesTargetKeysAndFreshLocalGeneration(t *testing.T) {
	source, key := profileFixture(t)
	bundleID, _ := NewID()
	source.Bundles = []Bundle{{ID: bundleID, Name: "api", ValueIDs: []string{source.Values[0].ID}}}
	expiry := time.Now().Add(-time.Hour).Unix()
	source.Values[0].Slots[1].ExpiresAt = &expiry
	if err := source.Sign(key); err != nil {
		t.Fatal(err)
	}
	exported, err := source.ExportLogical(key)
	if err != nil {
		t.Fatal(err)
	}
	defer exported.Destroy()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	encrypt, err := age.Encrypt(&encrypted, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	// Only this deliberate age boundary sees the owned logical plaintext.
	if _, err = encrypt.Write(exported.data); err != nil {
		t.Fatal(err)
	}
	if err = encrypt.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("synthetic-common-token")) {
		t.Fatal("plaintext age archive")
	}
	decrypt, err := age.Decrypt(bytes.NewReader(encrypted.Bytes()), identity)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(decrypt)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("logical payload invalid")
	}
	for _, name := range []string{"vault_id", "profile_id", "storage_generation", "key_epoch", "wrapped_key", "session_id", "launch", "process"} {
		if _, exists := fields[name]; exists {
			t.Fatal("device-local material exported", name)
		}
	}
	input := NewMaterial(raw)
	defer input.Destroy()
	targetMaster, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer targetMaster.Destroy()
	targetVault, _ := NewID()
	target, targetKey, err := ImportLogical(input, "renamed", targetVault, targetMaster)
	if err != nil {
		t.Fatal(err)
	}
	defer targetKey.Destroy()
	if target.Name != "renamed" || target.ID == source.ID || target.Generation == source.Generation || target.VaultID == source.VaultID || target.VaultID != targetVault || sameMaterial(key, targetKey) {
		t.Fatal("source device keys or incarnation reused")
	}
	unwrapped, err := UnwrapKeyset(targetMaster, targetVault, target.ID, target.KeyEpoch, target.WrappedKey)
	if err != nil {
		t.Fatal(err)
	}
	defer unwrapped.Destroy()
	if !sameMaterial(unwrapped, targetKey) {
		t.Fatal("target keyset cannot be reopened")
	}
	expected, refs, err := source.Environment("", key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	actual, newRefs, err := target.Environment("", unwrapped, time.Now())
	if err != nil || !reflect.DeepEqual(expected, actual) || !reflect.DeepEqual(refs, newRefs) || !reflect.DeepEqual(source.Bundles, target.Bundles) {
		t.Fatal("logical values, versions or bundle membership changed", err)
	}
	if _, _, err = target.Environment("local", targetKey, time.Now()); err == nil || err.Error() != "value_expired" {
		t.Fatal("expired override was extended or replaced by common", err)
	}
	if _, _, err = target.Environment("", key, time.Now()); !errors.Is(err, ErrIntegrity) {
		t.Fatal("source key unlocks destination profile", err)
	}
}

func TestLogicalRestoreKeepsSealedStateAndPublicNeedsNoVault(t *testing.T) {
	sealed, key := profileFixture(t)
	sealed.Values = nil
	sealed.Bundles = []Bundle{}
	if err := sealed.Sign(key); err != nil {
		t.Fatal(err)
	}
	b, err := sealed.ExportLogical(key)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	master, _ := NewKey()
	defer master.Destroy()
	vid, _ := NewID()
	p, k, err := ImportLogical(b, "empty-sealed", vid, master)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Destroy()
	if !p.Sealed || k == nil {
		t.Fatal("sealed profile downgraded on transfer")
	}
	public, err := NewProfile("public")
	if err != nil {
		t.Fatal(err)
	}
	public.Set("READY", Public, "", "yes", nil)
	plain, err := public.ExportLogical(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Destroy()
	q, qkey, err := ImportLogical(plain, "public-copy", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if q.Sealed || qkey != nil || q.VaultID != "" {
		t.Fatal("public transfer initializes vault")
	}
	malformed := NewMaterial(bytes.Replace(plain.data, []byte(`"sealed":false`), []byte(`"sealed":false,"session_id":"unsupported"`), 1))
	defer malformed.Destroy()
	if _, _, err = ImportLogical(malformed, "bad", "", nil); err == nil {
		t.Fatal("unknown runtime state imported")
	}
}

func TestLogicalImportRejectsMissingOrNullValueAndSealedState(t *testing.T) {
	p, _ := NewProfile("public")
	p.Set("READY", Public, "", "yes", nil)
	exported, err := p.ExportLogical(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer exported.Destroy()
	for _, change := range [][2]string{{`"value":"yes"`, `"value":null`}, {`"value":"yes"`, ``}, {`"sealed":false,`, ``}, {`"sealed":false`, `"sealed":null`}} {
		raw := bytes.Replace(exported.data, []byte(change[0]), []byte(change[1]), 1)
		if change[1] == "" && change[0] == `"value":"yes"` {
			raw = bytes.Replace(raw, []byte(`,}`), []byte(`}`), 1)
		}
		if !json.Valid(raw) {
			t.Fatal("fixture must be syntactically valid JSON")
		}
		material := NewMaterial(raw)
		clear(raw)
		if _, _, err := ImportLogical(material, "copy", "", nil); err == nil {
			material.Destroy()
			t.Fatal("missing logical field became empty value or public state")
		}
		material.Destroy()
	}
}
