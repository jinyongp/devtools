package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/profilekey"
)

func profileFixture(t *testing.T) (*Profile, *Material) {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	k := NewMaterial(bytes.Repeat([]byte{0x42}, 32))
	t.Cleanup(k.Destroy)
	master := NewMaterial(bytes.Repeat([]byte{0x17}, 32))
	defer master.Destroy()
	p, err := NewSealedProfile("app", id, master, k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.CreateEnv("local", k); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		name       string
		kind       ValueKind
		env, value string
	}{
		{"TOKEN", Secret, "", "synthetic-common-token"}, {"TOKEN", Secret, "local", "synthetic-local-token"},
		{"URL", Public, "", "https://local.invalid"},
	} {
		if _, err = p.Set(v.name, v.kind, v.env, v.value, k); err != nil {
			t.Fatal(err)
		}
	}
	return p, k
}

func TestSealedSnapshotConfidentialityAndLayering(t *testing.T) {
	p, k := profileFixture(t)
	b, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("synthetic-")) {
		t.Fatal("secret in disk representation")
	}
	q, err := DecodeProfile(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, env := range []string{"", "local"} {
		out, refs, err := q.Environment(env, k, time.Now())
		if err != nil || len(refs) != 2 {
			t.Fatalf("environment: %v", err)
		}
		want := "synthetic-common-token"
		if env != "" {
			want = "synthetic-local-token"
		}
		if out["TOKEN"] != want || out["URL"] != "https://local.invalid" {
			t.Fatal("incorrect effective input")
		}
	}
	before, _ := q.Encode()
	changed, err := q.Set("TOKEN", Secret, "local", "synthetic-local-token", k)
	after, _ := q.Encode()
	if changed || err != nil || !bytes.Equal(before, after) {
		t.Fatal("same value changed version or ciphertext")
	}
	if _, err = q.Set("TOKEN", Public, "", "public", k); err == nil {
		t.Fatal("secret kind downgraded")
	}
	if _, err = q.Set("TOKEN", Secret, "local", "", k); err != nil {
		t.Fatal(err)
	}
	out, _, err := q.Environment("local", k, time.Now())
	if err != nil || out["TOKEN"] != "" {
		t.Fatal("empty override lost")
	}
	q.Unset("TOKEN", Secret, "local", k)
	out, _, err = q.Environment("local", k, time.Now())
	if err != nil || out["TOKEN"] != "synthetic-common-token" {
		t.Fatal("unset did not restore common")
	}
	q.Unset("TOKEN", Secret, "", k)
	if !q.Sealed {
		t.Fatal("sealed state downgraded")
	}
	if _, _, err = q.Environment("", nil, time.Now()); !errors.Is(err, ErrLocked) {
		t.Fatal("locked sealed execution allowed")
	}
	if value, verified, err := q.PublicValue("URL", "", nil, time.Now()); err != nil || verified || value != "https://local.invalid" {
		t.Fatal("locked public read contract")
	}
}

func TestManifestAuthenticatesContentMembershipAndIdentity(t *testing.T) {
	p, k := profileFixture(t)
	changes := map[string]func(*Profile){
		"public value": func(q *Profile) {
			for i := range q.Values {
				if q.Values[i].Kind == Public {
					x := "https://attacker.invalid"
					q.Values[i].Slots[0].PublicValue = &x
				}
			}
		},
		"slot deletion":    func(q *Profile) { q.Values[0].Slots = q.Values[0].Slots[:1] },
		"value deletion":   func(q *Profile) { q.Values = q.Values[1:] },
		"profile identity": func(q *Profile) { q.ID = strings.Repeat("a", 32) },
		"generation":       func(q *Profile) { q.Generation = strings.Repeat("b", 32) },
		"epoch":            func(q *Profile) { q.KeyEpoch++ },
		"nonce":            func(q *Profile) { q.Values[0].Slots[0].Nonce[0] ^= 1 },
		"ciphertext":       func(q *Profile) { q.Values[0].Slots[0].Ciphertext[0] ^= 1 },
		"membership": func(q *Profile) {
			q.Bundles = []Bundle{{ID: strings.Repeat("c", 32), Name: "backend", ValueIDs: []string{q.Values[0].ID}}}
		},
		"kind": func(q *Profile) { q.Values[0].Kind = Public },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			q, err := p.clone()
			if err != nil {
				t.Fatal(err)
			}
			change(q)
			if _, _, err = q.Environment("local", k, time.Now()); err == nil {
				t.Fatal("manifest change accepted")
			}
		})
	}
	wrong := NewMaterial(bytes.Repeat([]byte{1}, 32))
	defer wrong.Destroy()
	if err := p.Verify(wrong); !errors.Is(err, ErrIntegrity) {
		t.Fatal("wrong key accepted")
	}
	a, _ := derive(k, p.VaultID, p.ID, "encryption")
	b, _ := derive(k, p.VaultID, p.ID, "manifest")
	defer clear(a)
	defer clear(b)
	if bytes.Equal(a, b) {
		t.Fatal("purpose keys equal")
	}
	q, _ := p.clone()
	q.ID = strings.Repeat("d", 32)
	q.Sign(k)
	if _, _, err := q.Environment("local", k, time.Now()); !errors.Is(err, ErrIntegrity) {
		t.Fatal("slot ciphertext moved to another profile")
	}
}

func TestNonceFailureDoesNotCommitAndExpiredOverrideDoesNotFallback(t *testing.T) {
	p, k := profileFixture(t)
	before, _ := p.Encode()
	// Existing slot: consume the new version ID, then fail before a nonce exists.
	if _, err := p.set("TOKEN", Secret, "local", "replacement", k, bytes.NewReader(make([]byte, 16))); !errors.Is(err, io.EOF) {
		t.Fatalf("expected nonce failure: %v", err)
	}
	after, _ := p.Encode()
	if !bytes.Equal(before, after) {
		t.Fatal("failed mutation changed profile")
	}
	for i := range p.Values {
		for j := range p.Values[i].Slots {
			if p.Values[i].Slots[j].Env == "local" {
				past := int64(1)
				p.Values[i].Slots[j].ExpiresAt = &past
			}
		}
	}
	p.Sign(k)
	if _, _, err := p.Environment("local", k, time.Now()); err == nil || err.Error() != "value_expired" {
		t.Fatal("expired override fell back")
	}
}

func TestStrictProfileDecode(t *testing.T) {
	p, _ := profileFixture(t)
	b, _ := p.Encode()
	for _, input := range [][]byte{
		append(append([]byte{}, b...), []byte(" {}")...),
		bytes.Replace(b, []byte(`"vault_format":1`), []byte(`"vault_format":1,"vault_format":1`), 1),
		bytes.Replace(b, []byte(`"vault_format":1`), []byte(`"vault_format":1,"unknown":0`), 1),
		bytes.Replace(b, []byte(`"app"`), []byte{'"', 0xff, '"'}, 1),
	} {
		if _, err := DecodeProfile(input); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
}

func TestAuthenticatedStoreAtomicBatchAndMissingProfile(t *testing.T) {
	p, k := profileFixture(t)
	master := NewMaterial(bytes.Repeat([]byte{9}, 32))
	defer master.Destroy()
	s := FileStore{Root: filepath.Join(privateTempDir(t), "data"), VaultID: p.VaultID, CatalogKey: master}
	ctx := context.Background()
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, p, k); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, pair := range [][2]string{{"u1", "synthetic-batch-password-one"}, {"u2", "synthetic-batch-password-two"}} {
		pair := pair
		wg.Go(func() {
			_, err := s.Update(ctx, "app", k, func(q *Profile) (bool, error) {
				if _, err := q.Set("USER", Public, "", pair[0], k); err != nil {
					return false, err
				}
				return q.Set("PASSWORD", Secret, "", pair[1], k)
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	q, err := s.Read(ctx, "app", k)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := q.Environment("", k, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !((out["USER"] == "u1" && out["PASSWORD"] == "synthetic-batch-password-one") || (out["USER"] == "u2" && out["PASSWORD"] == "synthetic-batch-password-two")) {
		t.Fatal("mixed pair committed")
	}
	path := filepath.Join(s.Root, "profiles", profilekey.CanonicalFilename("app"))
	latest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(latest, []byte(out["PASSWORD"])) {
		t.Fatal("password persisted in plaintext")
	}
	// A whole old profile is valid on its own, but not against the current catalog.
	old, _ := p.Encode()
	if err = os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, "app", k); !errors.Is(err, ErrIntegrity) {
		t.Fatal("partial rollback accepted")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, "app", k); !errors.Is(err, ErrRecovery) {
		t.Fatal("missing registered profile became empty")
	}
}

func TestPublicOnlyAndSealedCatalogDowngrade(t *testing.T) {
	p, err := NewProfile("public")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := p.Set("MODE", Public, "", "dev", nil); err != nil || !changed {
		t.Fatal(err)
	}
	if out, _, err := p.Environment("", nil, time.Now()); err != nil || out["MODE"] != "dev" {
		t.Fatal("public-only requires vault")
	}
	sealed, k := profileFixture(t)
	master := NewMaterial(bytes.Repeat([]byte{3}, 32))
	defer master.Destroy()
	s := FileStore{Root: filepath.Join(privateTempDir(t), "data"), VaultID: sealed.VaultID, CatalogKey: master}
	ctx := context.Background()
	if err = s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Create(ctx, sealed, k); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	path := filepath.Join(s.Root, "profiles", profilekey.CanonicalFilename("app"))
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, "app", nil); !errors.Is(err, ErrIntegrity) {
		t.Fatal("sealed profile downgraded through replacement")
	}
}
