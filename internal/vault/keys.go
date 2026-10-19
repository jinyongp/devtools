package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	MaxKeySlotBytes    = 4 << 10
	MaxPassphraseBytes = 64 << 10
	passphrasePreset   = "argon2id-64m-t3-p4-v1"
	wrappedKeySize     = chacha20poly1305.NonceSizeX + 32 + chacha20poly1305.Overhead
)

type KDFParameters struct {
	Preset    string `json:"preset"`
	MemoryKiB uint32 `json:"memory_kib"`
	Passes    uint32 `json:"passes"`
	Lanes     uint8  `json:"lanes"`
	Salt      []byte `json:"salt"`
}
type passphraseSlot struct {
	Format     int           `json:"format"`
	Backend    string        `json:"backend"`
	VaultID    string        `json:"vault_id"`
	KDF        KDFParameters `json:"kdf"`
	Nonce      []byte        `json:"nonce"`
	Ciphertext []byte        `json:"ciphertext"`
}

// PassphraseProtector never persists the passphrase or the derived wrapping key.
// A finite allowlisted KDF is checked before any Argon2 memory allocation.
type PassphraseProtector struct {
	VaultID    string
	Passphrase *Material
	random     io.Reader
	kdf        func([]byte, []byte, uint32, uint32, uint8, uint32) []byte
}

func (p PassphraseProtector) Identity() ProtectionIdentity {
	return ProtectionIdentity{Backend: "passphrase", VaultID: p.VaultID}
}

func NewKey() (*Material, error) {
	b := make([]byte, 32)
	defer clear(b)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, err
	}
	return NewMaterial(b), nil
}
func (p PassphraseProtector) input() ([]byte, error) {
	if !validID(p.VaultID) {
		return nil, ErrInvalid
	}
	b := p.Passphrase.CopyBytes()
	if len(b) == 0 {
		clear(b)
		return nil, ErrLocked
	}
	if len(b) > MaxPassphraseBytes {
		clear(b)
		return nil, ErrTooLarge
	}
	if !utf8.Valid(b) || bytes.IndexAny(b, "\x00\r\n") >= 0 {
		clear(b)
		return nil, ErrInvalid
	}
	return b, nil
}
func (s passphraseSlot) validate(vaultID string) error {
	if s.Format != StorageVersion || s.Backend != "passphrase" || s.VaultID != vaultID {
		return ErrInvalid
	}
	k := s.KDF
	if k.Preset != passphrasePreset || k.MemoryKiB != 64*1024 || k.Passes != 3 || k.Lanes != 4 || len(k.Salt) != 16 {
		return ErrInvalid
	}
	if len(s.Nonce) != chacha20poly1305.NonceSizeX || len(s.Ciphertext) != 32+chacha20poly1305.Overhead {
		return ErrInvalid
	}
	return nil
}
func (s passphraseSlot) aad() ([]byte, error) {
	e := NewEncoder("vault-passphrase-wrap")
	e.Uint(uint64(s.Format))
	e.String(s.Backend)
	e.String(s.VaultID)
	e.String(s.KDF.Preset)
	e.Uint(uint64(s.KDF.MemoryKiB))
	e.Uint(uint64(s.KDF.Passes))
	e.Uint(uint64(s.KDF.Lanes))
	e.Blob(s.KDF.Salt)
	return e.Finish()
}
func (p PassphraseProtector) derive(passphrase []byte, k KDFParameters) []byte {
	f := p.kdf
	if f == nil {
		f = argon2.IDKey
	}
	return f(passphrase, k.Salt, k.Passes, k.MemoryKiB, k.Lanes, 32)
}
func (p PassphraseProtector) Wrap(ctx context.Context, key []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	passphrase, err := p.input()
	if err != nil {
		return nil, err
	}
	defer clear(passphrase)
	r := p.random
	if r == nil {
		r = rand.Reader
	}
	s := passphraseSlot{Format: StorageVersion, Backend: "passphrase", VaultID: p.VaultID, KDF: KDFParameters{Preset: passphrasePreset, MemoryKiB: 64 * 1024, Passes: 3, Lanes: 4, Salt: make([]byte, 16)}, Nonce: make([]byte, chacha20poly1305.NonceSizeX)}
	if _, err = io.ReadFull(r, s.KDF.Salt); err != nil {
		return nil, err
	}
	if _, err = io.ReadFull(r, s.Nonce); err != nil {
		return nil, err
	}
	k := p.derive(passphrase, s.KDF)
	defer clear(k)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	a, err := chacha20poly1305.NewX(k)
	if err != nil {
		return nil, ErrIntegrity
	}
	aad, err := s.aad()
	if err != nil {
		return nil, err
	}
	s.Ciphertext = a.Seal(nil, s.Nonce, key, aad)
	return json.Marshal(s)
}
func (p PassphraseProtector) Unwrap(ctx context.Context, wrapped []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(wrapped) > MaxKeySlotBytes {
		return nil, ErrTooLarge
	}
	var s passphraseSlot
	if err := StrictDecode(wrapped, &s); err != nil {
		return nil, err
	}
	if err := s.validate(p.VaultID); err != nil {
		return nil, err
	}
	passphrase, err := p.input()
	if err != nil {
		return nil, err
	}
	defer clear(passphrase)
	k := p.derive(passphrase, s.KDF)
	defer clear(k)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	a, err := chacha20poly1305.NewX(k)
	if err != nil {
		return nil, ErrIntegrity
	}
	aad, err := s.aad()
	if err != nil {
		return nil, err
	}
	b, err := a.Open(nil, s.Nonce, s.Ciphertext, aad)
	if err != nil {
		return nil, ErrIntegrity
	}
	return b, nil
}

func keysetAAD(vaultID, profileID string, epoch uint64) ([]byte, error) {
	if !validID(vaultID) || !validID(profileID) || epoch == 0 {
		return nil, ErrInvalid
	}
	e := NewEncoder("profile-keyset-wrap")
	e.Uint(StorageVersion)
	e.String(vaultID)
	e.String(profileID)
	e.Uint(epoch)
	return e.Finish()
}
func WrapKeyset(master *Material, vaultID, profileID string, epoch uint64, key *Material) ([]byte, error) {
	aad, err := keysetAAD(vaultID, profileID, epoch)
	if err != nil {
		return nil, err
	}
	k, err := derive(master, vaultID, profileID, "key-wrap")
	if err != nil {
		return nil, err
	}
	defer clear(k)
	plain := key.CopyBytes()
	defer clear(plain)
	if len(plain) != 32 {
		return nil, ErrInvalid
	}
	a, err := chacha20poly1305.NewX(k)
	if err != nil {
		return nil, ErrIntegrity
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, plain, aad), nil
}
func UnwrapKeyset(master *Material, vaultID, profileID string, epoch uint64, wrapped []byte) (*Material, error) {
	if len(wrapped) != wrappedKeySize {
		return nil, ErrInvalid
	}
	aad, err := keysetAAD(vaultID, profileID, epoch)
	if err != nil {
		return nil, err
	}
	k, err := derive(master, vaultID, profileID, "key-wrap")
	if err != nil {
		return nil, err
	}
	defer clear(k)
	a, err := chacha20poly1305.NewX(k)
	if err != nil {
		return nil, ErrIntegrity
	}
	plain, err := a.Open(nil, wrapped[:a.NonceSize()], wrapped[a.NonceSize():], aad)
	if err != nil {
		return nil, ErrIntegrity
	}
	defer clear(plain)
	return NewMaterial(plain), nil
}
