package values

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/vault"
)

// PublicLogicalSnapshotHeld exports public-only modern data without creating a
// vault. Legacy images stay portable through the existing encrypted-age path.
// The caller holds the data gate and checked public-root classification.
func PublicLogicalSnapshotHeld(ctx context.Context, root, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := profilekey.Snapshot(filepath.Join(root, "profiles"), name, "values", vault.MaxDocumentBytes)
	if err != nil || raw == nil {
		return raw, err
	}
	p, err := vault.DecodeProfile(raw)
	if err != nil {
		if _, err = InspectSnapshot(raw, name); err != nil {
			clear(raw)
			return nil, vault.ErrInvalid
		}
		return raw, nil
	}
	defer clear(raw)
	if p.Name != name {
		return nil, vault.ErrIntegrity
	}
	if p.Sealed {
		return nil, vault.ErrKeyMissing
	}
	logical, err := p.ExportLogical(nil)
	if err != nil {
		return nil, err
	}
	defer logical.Destroy()
	return logical.CopyBytes(), nil
}

// PreparePublicRestoreHeld writes fresh local identities/generation for public
// logical values. An archive containing secrets or sealed history requires an
// explicitly initialized target vault and cannot create plaintext secret state.
func PreparePublicRestoreHeld(ctx context.Context, root, name string, data []byte) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory := filepath.Join(root, "profiles")
	if data == nil {
		return profilekey.Replacement(directory, name, "values", nil)
	}
	var logical *vault.Material
	if IsLogicalSnapshot(data) {
		logical = vault.NewMaterial(data)
	} else {
		state, err := InspectSnapshot(data, name)
		if err != nil {
			return nil, vault.ErrInvalid
		}
		if hasSecret(state) {
			return nil, vault.ErrKeyMissing
		}
		p, err := convertLegacy(state, "", nil)
		if err != nil {
			return nil, err
		}
		logical, err = p.ExportLogical(nil)
		if err != nil {
			return nil, err
		}
	}
	defer logical.Destroy()
	p, key, err := vault.ImportLogical(logical, name, "", nil)
	key.Destroy()
	if err != nil {
		return nil, err
	}
	raw, err := p.Encode()
	if err != nil {
		return nil, err
	}
	return profilekey.Replacement(directory, name, "values", raw)
}

func PublicSnapshotIdentityHeld(root, name string) (vault.InputIdentity, error) {
	raw, err := profilekey.Snapshot(filepath.Join(root, "profiles"), name, "values", vault.MaxDocumentBytes)
	if err != nil {
		return vault.InputIdentity{}, err
	}
	defer clear(raw)
	if raw == nil {
		return vault.InputIdentity{}, nil
	}
	p, err := vault.DecodeProfile(raw)
	if err != nil {
		if _, err = InspectSnapshot(raw, name); err != nil {
			return vault.InputIdentity{}, vault.ErrInvalid
		}
		return vault.InputIdentity{}, nil
	}
	if p.Name != name || p.Sealed {
		return vault.InputIdentity{}, vault.ErrIntegrity
	}
	return vault.InputIdentity{ProfileID: p.ID, StorageGeneration: p.Generation}, nil
}
