package vault

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

// KeyFile contains only a backend-protected root key and routing metadata.
// A selected backend must remain selected after errors; callers do not fall back
// to another backend, a new key, or an unprotected local file.
type KeyFile struct {
	Format  int    `json:"key_file_format"`
	VaultID string `json:"vault_id"`
	Backend string `json:"backend"`
	Wrapped []byte `json:"wrapped"`
}

func (k KeyFile) validate() error {
	if k.Format != StorageVersion || !validID(k.VaultID) || len(k.Wrapped) == 0 || len(k.Wrapped) > MaxKeySlotBytes {
		return ErrInvalid
	}
	switch k.Backend {
	case "passphrase", "keychain", "dpapi", "secret-service":
		return nil
	}
	return ErrInvalid
}

type KeyFileStore struct{ Directory string }

const keyFilename = "key-slot.json"

func (s KeyFileStore) Read() (*KeyFile, error) {
	r, err := platformfs.OpenRoot(s.Directory, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrKeyMissing
	}
	if err != nil {
		return nil, ErrRecovery
	}
	defer r.Close()
	b, err := r.ReadFile(keyFilename, MaxKeySlotBytes*2)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrKeyMissing
	}
	if err != nil {
		return nil, ErrRecovery
	}
	var k KeyFile
	if err = StrictDecode(b, &k); err != nil {
		return nil, err
	}
	if err = k.validate(); err != nil {
		return nil, err
	}
	return &k, nil
}
func (s KeyFileStore) Initialize(ctx context.Context, vaultID, backend string, protector Protector) (*Material, error) {
	// Check before generating a key; an existing or unsafe key slot is never reset.
	if _, err := os.Lstat(filepath.Join(s.Directory, keyFilename)); err == nil {
		return nil, errorsNamed("vault_already_initialized")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRecovery
	}
	// A lost slot beside protected profile data is recovery, not a new vault.
	// Check before RNG/native item creation so setup cannot strand existing
	// encrypted profiles behind an unrelated replacement root key.
	if filepath.Base(s.Directory) == "vault" {
		dataRoot := filepath.Dir(s.Directory)
		if _, err := os.Lstat(filepath.Join(dataRoot, catalogRelative)); err == nil {
			return nil, ErrKeyMissing
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, ErrRecovery
		}
		if _, protected, err := maintenance.ProtectedRootHeld(dataRoot); err != nil {
			return nil, ErrRecovery
		} else if protected {
			return nil, ErrKeyMissing
		}
	}
	if protector == nil || !validID(vaultID) || protector.Identity() != (ProtectionIdentity{Backend: backend, VaultID: vaultID}) {
		return nil, ErrInvalid
	}
	k := KeyFile{Format: StorageVersion, VaultID: vaultID, Backend: backend, Wrapped: []byte{1}}
	if err := k.validate(); err != nil {
		return nil, err
	}
	master, err := NewKey()
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			master.Destroy()
		}
	}()
	plain := master.CopyBytes()
	defer clear(plain)
	k.Wrapped, err = protector.Wrap(ctx, plain)
	if err != nil {
		return nil, err
	}
	if err = k.validate(); err != nil {
		return nil, err
	}
	check, err := protector.Unwrap(ctx, k.Wrapped)
	if err != nil {
		return nil, err
	}
	same := hmac.Equal(plain, check)
	clear(check)
	if !same {
		return nil, ErrIntegrity
	}
	b, err := json.Marshal(k)
	if err != nil {
		return nil, ErrInvalid
	}
	if len(b) > MaxKeySlotBytes*2 {
		return nil, ErrTooLarge
	}
	// Root setup changes public launch classification too. Native wrapping
	// and actual unwrap verification have finished before taking either gate.
	if filepath.Base(s.Directory) == "vault" {
		finish, err := AcquireLaunchMutation(ctx, s.Directory)
		if err != nil {
			return nil, err
		}
		defer finish()
		release, err := maintenance.AcquireExclusive(ctx, filepath.Dir(s.Directory))
		if err != nil {
			return nil, err
		}
		defer release()
		if err = CheckPublicRootHeld(filepath.Dir(s.Directory)); err != nil {
			return nil, err
		}
	}
	r, err := platformfs.OpenRoot(s.Directory, true)
	if err != nil {
		return nil, ErrRecovery
	}
	defer r.Close()
	if err = r.WriteFile(ctx, keyFilename, b, false); err != nil {
		return nil, err
	}
	keep = true
	return master, nil
}
func (k *KeyFile) Unlock(ctx context.Context, backend string, protector Protector) (*Material, error) {
	if k == nil {
		return nil, ErrKeyMissing
	}
	if err := k.validate(); err != nil {
		return nil, err
	}
	if backend != k.Backend || protector == nil || protector.Identity() != (ProtectionIdentity{Backend: k.Backend, VaultID: k.VaultID}) {
		return nil, ErrUnavailable
	}
	b, err := protector.Unwrap(ctx, k.Wrapped)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	if len(b) != 32 {
		return nil, ErrIntegrity
	}
	return NewMaterial(b), nil
}
