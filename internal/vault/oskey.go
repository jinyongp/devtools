package vault

import (
	"bytes"
	"crypto/sha256"
	"errors"
)

var (
	ErrDenied              = errors.New("key_access_denied")
	ErrInteractionRequired = errors.New("key_interaction_required")
)

// Native key stores receive a typed, vault-bound payload. Validate it after
// unprotection too: success from an OS API alone is not a valid vault key.
func osKeyPayload(backend, vaultID string, key []byte) ([]byte, error) {
	if !validID(vaultID) || len(key) != 32 {
		return nil, ErrInvalid
	}
	e := NewEncoder("vault-os-key")
	e.Uint(StorageVersion)
	e.String(backend)
	e.String(vaultID)
	e.Blob(key)
	b, err := e.Finish()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(b)
	return append(b, digest[:]...), nil
}

func osKeyFromPayload(backend, vaultID string, b []byte) ([]byte, error) {
	// All envelope fields have fixed lengths for one expected backend/vault.
	blank, err := osKeyPayload(backend, vaultID, make([]byte, 32))
	if err != nil {
		return nil, err
	}
	defer clear(blank)
	if len(b) != len(blank) {
		return nil, ErrIntegrity
	}
	offset := len(b) - sha256.Size - 32
	expected, err := osKeyPayload(backend, vaultID, b[offset:offset+32])
	if err != nil {
		return nil, err
	}
	defer clear(expected)
	if !bytes.Equal(b, expected) {
		return nil, ErrIntegrity
	}
	return bytes.Clone(b[offset : offset+32]), nil
}
