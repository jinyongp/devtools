package vault

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// These internal material classes have disjoint HKDF and AAD domains. They are
// local recovery state, not an independent off-device backup or key escrow.
const (
	MaterialRecovery           = "migration-recovery"
	MaterialLegacyVerification = "legacy-verification"
	MaterialReceipt            = "receipt"
	MaterialRewrap             = "key-rewrap"
	MaxProtectedPlainBytes     = 64 << 20
)

type protectedMaterial struct {
	Format     int    `json:"protected_material_format"`
	VaultID    string `json:"vault_id"`
	Scope      string `json:"scope_id"`
	Purpose    string `json:"purpose"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func validMaterialPurpose(purpose string) bool {
	return purpose == MaterialRecovery || purpose == MaterialLegacyVerification || purpose == MaterialReceipt || purpose == MaterialRewrap
}
func (p protectedMaterial) aad() ([]byte, error) {
	if p.Format != StorageVersion || !validID(p.VaultID) || !validID(p.Scope) || !validMaterialPurpose(p.Purpose) {
		return nil, ErrInvalid
	}
	e := NewEncoder("vault-protected-material")
	e.Uint(uint64(p.Format))
	e.String(p.VaultID)
	e.String(p.Scope)
	e.String(p.Purpose)
	return e.Finish()
}

// SealProtected crosses an explicit cryptographic boundary without creating a
// plaintext disk file. The returned document contains ciphertext and metadata.
func SealProtected(ctx context.Context, master *Material, vaultID, scope, purpose string, plain *Material) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plain == nil {
		return nil, ErrInvalid
	}
	if len(plain.data) > MaxProtectedPlainBytes {
		return nil, ErrTooLarge
	}
	envelope := protectedMaterial{Format: StorageVersion, VaultID: vaultID, Scope: scope, Purpose: purpose, Nonce: make([]byte, chacha20poly1305.NonceSizeX)}
	aad, err := envelope.aad()
	if err != nil {
		return nil, err
	}
	key, err := derive(master, vaultID, scope, "material/"+purpose)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	cipher, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = io.ReadFull(rand.Reader, envelope.Nonce); err != nil {
		return nil, err
	}
	envelope.Ciphertext = cipher.Seal(nil, envelope.Nonce, plain.data, aad)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil || len(encoded) > MaxDocumentBytes {
		return nil, ErrTooLarge
	}
	return encoded, nil
}

// OpenProtected authenticates expected identity and purpose before returning an
// owned plaintext buffer. Missing/damaged material is never an empty key/state.
func OpenProtected(ctx context.Context, master *Material, vaultID, scope, purpose string, encoded []byte) (*Material, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var envelope protectedMaterial
	if err := StrictDecode(encoded, &envelope); err != nil {
		return nil, err
	}
	if envelope.VaultID != vaultID || envelope.Scope != scope || envelope.Purpose != purpose {
		return nil, ErrIntegrity
	}
	aad, err := envelope.aad()
	if err != nil {
		return nil, err
	}
	if len(envelope.Nonce) != chacha20poly1305.NonceSizeX || len(envelope.Ciphertext) < chacha20poly1305.Overhead || len(envelope.Ciphertext) > MaxProtectedPlainBytes+chacha20poly1305.Overhead {
		return nil, ErrInvalid
	}
	key, err := derive(master, vaultID, scope, "material/"+purpose)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	cipher, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrInvalid
	}
	plain, err := cipher.Open(nil, envelope.Nonce, envelope.Ciphertext, aad)
	if err != nil {
		return nil, ErrIntegrity
	}
	defer clear(plain)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return NewMaterial(plain), nil
}
