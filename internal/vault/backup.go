package vault

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/profilekey"
)

// BackupFingerprint authenticates the private preview with a domain-separated
// root key. A restore can target a profile that does not yet have a keyset.
func BackupFingerprint(master *Material, vaultID, target string, payload []byte) (string, error) {
	if !validID(vaultID) || !namePattern.MatchString(target) {
		return "", ErrInvalid
	}
	key, err := derive(master, vaultID, "backup/"+target, "backup-preview")
	if err != nil {
		return "", err
	}
	defer clear(key)
	h := hmac.New(sha256.New, key)
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// LogicalSnapshotHeld requires the data-root maintenance gate and an already
// acquired root key. Unsigned public profiles outside the sealed catalog remain
// public-only; a missing/damaged sealed catalog entry is never an empty profile.
func (s FileStore) LogicalSnapshotHeld(ctx context.Context, name string) ([]byte, error) {
	p, key, err := s.ReadAnyWithMasterHeld(ctx, name, s.CatalogKey)
	if err != nil {
		if err.Error() == "profile_not_found" {
			return nil, nil
		}
		return nil, err
	}
	defer key.Destroy()
	portable, err := p.ExportLogical(key)
	if err != nil {
		return nil, err
	}
	defer portable.Destroy()
	return portable.CopyBytes(), nil
}

// LogicalReplacementHeld prepares target-local encrypted after-images. The
// caller owns the exclusive launch mutation lease, then the maintenance gate,
// and publishes these files together with task state and the restore receipt.
func (s FileStore) LogicalReplacementHeld(ctx context.Context, name string, input *Material) (map[string][]byte, error) {
	if !namePattern.MatchString(name) {
		return nil, ErrInvalid
	}
	catalog, err := s.readCatalog()
	if err != nil {
		return nil, err
	}
	index := -1
	sealed := false
	for i, entry := range catalog.Entries {
		if entry.Name == name {
			index = i
			sealed = entry.Sealed
			break
		}
	}
	historyFiles := map[string][]byte{}
	if sealed {
		old, oldKey, readErr := s.ReadWithMasterHeld(ctx, name, s.CatalogKey)
		if readErr != nil {
			return nil, readErr
		}
		historyFiles, err = s.receiptHistoryFiles(ctx, old, oldKey, time.Now())
		oldKey.Destroy()
		if err != nil {
			return nil, err
		}
	}
	if input == nil {
		if sealed {
			yes := true
			b, _ := json.Marshal(logicalProfile{Format: 1, Profile: name, Sealed: &yes, Envs: []string{}, Values: []logicalValue{}, Bundles: []Bundle{}})
			input = NewMaterial(b)
			clear(b)
			defer input.Destroy()
		} else {
			if index >= 0 {
				catalog.Entries = append(catalog.Entries[:index], catalog.Entries[index+1:]...)
			}
			if err = catalog.Sign(s.CatalogKey); err != nil {
				return nil, err
			}
			cb, err := json.Marshal(catalog)
			if err != nil {
				return nil, ErrInvalid
			}
			files, err := profilekey.Replacement(filepath.Join(s.Root, "profiles"), name, "values", nil)
			if err != nil {
				return nil, ErrRecovery
			}
			files[catalogRelative] = cb
			return files, nil
		}
	}
	p, key, err := importLogical(input, name, s.VaultID, s.CatalogKey, sealed)
	if err != nil {
		return nil, err
	}
	defer key.Destroy()
	if p.Sealed {
		if index < 0 {
			catalog.Entries = append(catalog.Entries, entryFor(p))
		} else {
			catalog.Entries[index] = entryFor(p)
		}
	} else if index >= 0 {
		catalog.Entries = append(catalog.Entries[:index], catalog.Entries[index+1:]...)
	}
	if err = catalog.Sign(s.CatalogKey); err != nil {
		return nil, err
	}
	cb, err := json.Marshal(catalog)
	if err != nil {
		return nil, ErrInvalid
	}
	pb, err := p.Encode()
	if err != nil {
		return nil, err
	}
	files, err := profilekey.Replacement(filepath.Join(s.Root, "profiles"), name, "values", pb)
	if err != nil {
		return nil, ErrRecovery
	}
	files[catalogRelative] = cb
	for path, data := range historyFiles {
		files[path] = data
	}
	return files, nil
}

// SnapshotIdentityHeld is local preview metadata, never portable archive data.
// Include the incarnation so restoring identical logical bytes cannot revive a
// stale preview from an earlier storage generation.
func (s FileStore) SnapshotIdentityHeld(ctx context.Context, name string) (InputIdentity, error) {
	if err := ctx.Err(); err != nil {
		return InputIdentity{}, err
	}
	catalog, err := s.readCatalog()
	if err != nil {
		return InputIdentity{}, err
	}
	for _, entry := range catalog.Entries {
		if entry.Name == name {
			return InputIdentity{ProfileID: entry.ID, StorageGeneration: entry.Generation}, nil
		}
	}
	raw, err := profilekey.Snapshot(filepath.Join(s.Root, "profiles"), name, "values", MaxDocumentBytes)
	if err != nil {
		return InputIdentity{}, ErrRecovery
	}
	if raw == nil {
		return InputIdentity{}, nil
	}
	p, err := DecodeProfile(raw)
	if err != nil {
		return InputIdentity{}, err
	}
	if p.Sealed {
		return InputIdentity{}, ErrIntegrity
	}
	return InputIdentity{ProfileID: p.ID, StorageGeneration: p.Generation}, nil
}
