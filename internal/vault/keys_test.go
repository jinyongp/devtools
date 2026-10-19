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
	"testing"
)

func protectorFixture(t *testing.T) PassphraseProtector {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	pass := NewMaterial([]byte("synthetic-unlock-passphrase-only"))
	t.Cleanup(pass.Destroy)
	return PassphraseProtector{VaultID: id, Passphrase: pass}
}
func TestPassphraseRealPresetRoundtripAndFailure(t *testing.T) {
	p := protectorFixture(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x5a}, 32)
	wrapped, err := p.Wrap(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrapped, key) || bytes.Contains(wrapped, []byte("synthetic-unlock")) {
		t.Fatal("plaintext key or passphrase persisted")
	}
	out, err := p.Unwrap(ctx, wrapped)
	if err != nil || !bytes.Equal(out, key) {
		t.Fatal("real preset roundtrip failed")
	}
	clear(out)
	wrong := p
	wrong.Passphrase = NewMaterial([]byte("wrong-synthetic-passphrase"))
	defer wrong.Passphrase.Destroy()
	if out, err := wrong.Unwrap(ctx, wrapped); out != nil || !errors.Is(err, ErrIntegrity) {
		t.Fatal("wrong passphrase accepted")
	}
	var slot passphraseSlot
	if err = json.Unmarshal(wrapped, &slot); err != nil {
		t.Fatal(err)
	}
	slot.Ciphertext[0] ^= 1
	damaged, _ := json.Marshal(slot)
	if out, err := p.Unwrap(ctx, damaged); out != nil || !errors.Is(err, ErrIntegrity) {
		t.Fatal("corrupt wrap accepted")
	}
	wrong = p
	wrong.VaultID = strings.Repeat("a", 32)
	if out, err := wrong.Unwrap(ctx, wrapped); out != nil || err == nil {
		t.Fatal("other vault accepted")
	}
}
func TestKDFHeaderRejectedBeforeDerivation(t *testing.T) {
	p := protectorFixture(t)
	called := 0
	p.kdf = func([]byte, []byte, uint32, uint32, uint8, uint32) []byte { called++; return make([]byte, 32) }
	source := passphraseSlot{Format: StorageVersion, Backend: "passphrase", VaultID: p.VaultID, KDF: KDFParameters{Preset: passphrasePreset, MemoryKiB: 64 * 1024, Passes: 3, Lanes: 4, Salt: make([]byte, 16)}, Nonce: make([]byte, 24), Ciphertext: make([]byte, 48)}
	for _, mutate := range []func(*passphraseSlot){
		func(s *passphraseSlot) { s.KDF.MemoryKiB = ^uint32(0) }, func(s *passphraseSlot) { s.KDF.Passes = 0 },
		func(s *passphraseSlot) { s.KDF.Lanes = 255 }, func(s *passphraseSlot) { s.KDF.Preset = "unknown" },
		func(s *passphraseSlot) { s.KDF.Salt = nil }, func(s *passphraseSlot) { s.Nonce = nil },
		func(s *passphraseSlot) { s.Ciphertext = nil }, func(s *passphraseSlot) { s.Backend = "dpapi" },
	} {
		s := source
		mutate(&s)
		b, _ := json.Marshal(s)
		if out, err := p.Unwrap(context.Background(), b); out != nil || err == nil {
			t.Fatal("invalid header accepted")
		}
	}
	for _, b := range [][]byte{
		bytes.Repeat([]byte{'x'}, MaxKeySlotBytes+1),
		[]byte(`{"format":1,"format":1}`), []byte(`{"format":1,"kdf":{"memory_kib":-1}}`),
	} {
		if out, err := p.Unwrap(context.Background(), b); out != nil || err == nil {
			t.Fatal("ambiguous header accepted")
		}
	}
	if called != 0 {
		t.Fatal("untrusted KDF parameters reached derivation")
	}
}
func TestPassphraseCancellationRNGAndInvalidInput(t *testing.T) {
	p := protectorFixture(t)
	p.random = bytes.NewReader(nil)
	if out, err := p.Wrap(context.Background(), make([]byte, 32)); out != nil || !errors.Is(err, io.EOF) {
		t.Fatal("RNG failure accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := p.Wrap(canceled, make([]byte, 32)); out != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled KDF proceeded")
	}
	for _, pass := range [][]byte{nil, {'a', '\n', 'b'}, {0xff}, bytes.Repeat([]byte{'x'}, MaxPassphraseBytes+1)} {
		p.Passphrase = NewMaterial(pass)
		if out, err := p.Wrap(context.Background(), make([]byte, 32)); out != nil || err == nil {
			t.Fatal("invalid passphrase accepted")
		}
		p.Passphrase.Destroy()
	}
}
func TestKeysetContextAndStableProfileWrapping(t *testing.T) {
	master, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	vaultID, _ := NewID()
	p, err := NewSealedProfile("app", vaultID, master, key)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnwrapKeyset(master, vaultID, p.ID, p.KeyEpoch, p.WrappedKey)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Destroy()
	a, b := out.CopyBytes(), key.CopyBytes()
	defer clear(a)
	defer clear(b)
	if !bytes.Equal(a, b) {
		t.Fatal("wrong profile key")
	}
	for _, input := range []struct {
		vault, profile string
		epoch          uint64
	}{
		{strings.Repeat("b", 32), p.ID, p.KeyEpoch}, {vaultID, strings.Repeat("c", 32), p.KeyEpoch}, {vaultID, p.ID, p.KeyEpoch + 1},
	} {
		if value, err := UnwrapKeyset(master, input.vault, input.profile, input.epoch, p.WrappedKey); value != nil || err == nil {
			t.Fatal("keyset context changed without failure")
		}
	}
}
func TestKeyFileInitializeVerifyAndNoImplicitReset(t *testing.T) {
	p := protectorFixture(t)
	s := KeyFileStore{Directory: filepath.Join(t.TempDir(), "vault")}
	ctx := context.Background()
	if _, err := s.Read(); !errors.Is(err, ErrKeyMissing) {
		t.Fatal("missing key was initialized")
	}
	master, err := s.Initialize(ctx, p.VaultID, "passphrase", p)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	k, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Directory, keyFilename))
	if err != nil {
		t.Fatal(err)
	}
	plain := master.CopyBytes()
	defer clear(plain)
	if bytes.Contains(data, plain) || bytes.Contains(data, []byte("synthetic-unlock")) {
		t.Fatal("key material in key file")
	}
	opened, err := k.Unlock(ctx, "passphrase", p)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Destroy()
	copy := opened.CopyBytes()
	defer clear(copy)
	if !bytes.Equal(copy, plain) {
		t.Fatal("persisted key not recoverable")
	}
	if _, err = s.Initialize(ctx, p.VaultID, "passphrase", p); err == nil {
		t.Fatal("existing vault reset")
	}
	if _, err = k.Unlock(ctx, "dpapi", p); !errors.Is(err, ErrUnavailable) {
		t.Fatal("backend changed silently")
	}
	p.VaultID = strings.Repeat("d", 32)
	if _, err = k.Unlock(ctx, "passphrase", p); !errors.Is(err, ErrUnavailable) {
		t.Fatal("protector identity ignored")
	}
	other := KeyFileStore{Directory: filepath.Join(t.TempDir(), "vault")}
	if _, err = other.Initialize(ctx, k.VaultID, "passphrase", p); !errors.Is(err, ErrInvalid) {
		t.Fatal("mismatched protector persisted")
	}
	if _, err = other.Read(); !errors.Is(err, ErrKeyMissing) {
		t.Fatal("failed preparation left a key")
	}
}
