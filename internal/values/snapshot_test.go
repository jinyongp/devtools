package values

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/vault"
)

func vaultSnapshotFixture(t *testing.T, count int) (string, vault.FileStore, *vault.Material, *vault.Material) {
	t.Helper()
	base, err := filepath.EvalSymlinks(privateTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "data")
	id, err := vault.NewID()
	if err != nil {
		t.Fatal(err)
	}
	pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Initialize(context.Background(), id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(master.Destroy)
	profileKey, err := vault.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(profileKey.Destroy)
	p, err := vault.NewSealedProfile("app", id, master, profileKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.CreateEnv("local", profileKey); err != nil {
		t.Fatal(err)
	}
	for i := range count {
		if _, err = p.Set(fmt.Sprintf("SECRET_%04d", i), vault.Secret, "", fmt.Sprintf("synthetic-slot-value-%04d", i), profileKey); err != nil {
			t.Fatal(err)
		}
	}
	store := vault.FileStore{Root: root, VaultID: id, CatalogKey: master}
	if err = store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = store.Create(context.Background(), p, profileKey); err != nil {
		t.Fatal(err)
	}
	return root, store, master, profileKey
}
func snapshotMaster(master *vault.Material, counter *atomic.Int32) func(context.Context) (*vault.Material, error) {
	return func(context.Context) (*vault.Material, error) {
		counter.Add(1)
		b := master.CopyBytes()
		defer clear(b)
		return vault.NewMaterial(b), nil
	}
}

func TestVaultSnapshotsUseOneKeyAccessForManySlots(t *testing.T) {
	for _, count := range []int{10, 100, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			root, _, master, _ := vaultSnapshotFixture(t, count)
			var calls atomic.Int32
			s, e := CaptureVault(context.Background(), root, "app", "local", true, time.Now(), snapshotMaster(master, &calls))
			if e != nil {
				t.Fatal(e)
			}
			defer s.Destroy()
			state := s.StateCopy()
			env, e := state.Environment("local")
			if e != nil {
				t.Fatal(e)
			}
			if len(env) != count || len(s.Metadata().Slots) != count || calls.Load() != 1 {
				t.Fatal("snapshot omitted slots or authenticated per slot")
			}
			if env["SECRET_0000"] != "synthetic-slot-value-0000" || env[fmt.Sprintf("SECRET_%04d", count-1)] != fmt.Sprintf("synthetic-slot-value-%04d", count-1) {
				t.Fatal("wrong effective values")
			}
			if !s.Metadata().IntegrityVerified {
				t.Fatal("sealed snapshot was not verified")
			}
			if _, err := json.Marshal(s); err == nil || fmt.Sprintf("%+v", s) != "[REDACTED]" {
				t.Fatal("snapshot can leak through diagnostics")
			}
			encoded, err := json.Marshal(s.Metadata())
			if err != nil || bytes.Contains(encoded, []byte("synthetic-slot-value")) {
				t.Fatal("metadata exposed plaintext")
			}
		})
	}
}

func TestVaultSnapshotBatchCannotMixAndRemainsFrozen(t *testing.T) {
	root, store, master, key := vaultSnapshotFixture(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	setPair := func(value string) error {
		_, err := store.Update(ctx, "app", key, func(p *vault.Profile) (bool, error) {
			if _, err := p.Set("PAIR_A", vault.Secret, "", value, key); err != nil {
				return false, err
			}
			return p.Set("PAIR_B", vault.Secret, "", value, key)
		})
		return err
	}
	if err := setPair("synthetic-initial-pair"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	frozen, e := CaptureVault(ctx, root, "app", "", true, time.Now(), snapshotMaster(master, &calls))
	if e != nil {
		t.Fatal(e)
	}
	defer frozen.Destroy()
	var writers sync.WaitGroup
	for _, prefix := range []string{"left", "right"} {
		writers.Go(func() {
			for i := range 4 {
				if err := setPair(fmt.Sprintf("synthetic-%s-%d", prefix, i)); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	for range 12 {
		s, e := CaptureVault(ctx, root, "app", "", true, time.Now(), snapshotMaster(master, &calls))
		if e != nil {
			t.Fatal(e)
		}
		env, e := s.StateCopy().Environment("")
		s.Destroy()
		if e != nil || env["PAIR_A"] != env["PAIR_B"] {
			t.Fatal("mixed batch snapshot")
		}
	}
	writers.Wait()
	old, e := frozen.StateCopy().Environment("")
	if e != nil || old["PAIR_A"] != "synthetic-initial-pair" || old["PAIR_B"] != old["PAIR_A"] {
		t.Fatal("frozen snapshot reread newer values")
	}
}

func TestVaultSnapshotEmptyOverrideExpiryAndMetadataOnly(t *testing.T) {
	root, store, master, key := vaultSnapshotFixture(t, 1)
	ctx := context.Background()
	_, err := store.Update(ctx, "app", key, func(p *vault.Profile) (bool, error) {
		if _, err := p.Set("SECRET_0000", vault.Secret, "local", "", key); err != nil {
			return false, err
		}
		return p.Set("ADDRESS", vault.Public, "", "https://snapshot.invalid", key)
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls))
	if e != nil {
		t.Fatal(e)
	}
	env, e := s.StateCopy().Environment("local")
	s.Destroy()
	if value, exists := env["SECRET_0000"]; e != nil || !exists || value != "" {
		t.Fatal("empty override fell back")
	}
	_, err = store.Update(ctx, "app", key, func(p *vault.Profile) (bool, error) {
		for i := range p.Values {
			if p.Values[i].Name == "SECRET_0000" {
				for j := range p.Values[i].Slots {
					if p.Values[i].Slots[j].Env == "local" {
						past := int64(1)
						p.Values[i].Slots[j].ExpiresAt = &past
					}
				}
			}
		}
		p.Revision++
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if s, e = CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls)); e == nil || e.Code != "value_expired" {
		s.Destroy()
		t.Fatal("expired override fell back")
	}
	// A binding-only snapshot verifies all manifest bytes, but doesn't need to
	// decrypt or deliver expired secret slots.
	s, e = CaptureVault(ctx, root, "app", "local", false, time.Now(), snapshotMaster(master, &calls))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Destroy()
	public, _, e := s.StateCopy().GetVariable("ADDRESS", "local")
	if e != nil || public != "https://snapshot.invalid" || s.CanInject() {
		t.Fatal("metadata-only snapshot exposed secret material")
	}
	path := filepath.Join(root, "profiles", profilekey.CanonicalFilename("app"))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := vault.DecodeProfile(b)
	if err != nil {
		t.Fatal(err)
	}
	for i := range p.Values {
		if p.Values[i].Kind == vault.Secret {
			p.Values[i].Slots[0].Ciphertext[0] ^= 1
			break
		}
	}
	b, err = p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if s, e = CaptureVault(ctx, root, "app", "local", false, time.Now(), snapshotMaster(master, &calls)); e == nil || e.Code != "integrity_failed" {
		s.Destroy()
		t.Fatal("metadata-only read ignored manifest corruption")
	}
}

func TestProfileKeyRotationDoesNotChangeEffectiveInputRevision(t *testing.T) {
	root, store, master, _ := vaultSnapshotFixture(t, 2)
	ctx := context.Background()
	var calls atomic.Int32
	before, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls))
	if e != nil {
		t.Fatal(e)
	}
	defer before.Destroy()
	if _, err := store.RotateProfile(ctx, "app", master); err != nil {
		t.Fatal(err)
	}
	after, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls))
	if e != nil {
		t.Fatal(e)
	}
	defer after.Destroy()
	if before.Metadata().Identity != after.Metadata().Identity || after.Metadata().KeyEpoch != before.Metadata().KeyEpoch+1 {
		t.Fatal("key rotation changed value identity or missed key epoch")
	}
	if calls.Load() != 2 {
		t.Fatal("key lookup occurred per slot")
	}
}
