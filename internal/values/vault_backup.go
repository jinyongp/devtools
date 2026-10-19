package values

import (
	"context"
	"github.com/jinyongp/devtools/internal/vault"
)

// PrepareVaultRestoreHeld keeps the logical archive private and creates only
// target-local encrypted after-images. Root/session keys are acquired by the
// caller before the launch mutation lease and maintenance gate.
func PrepareVaultRestoreHeld(ctx context.Context, root, vaultID string, master *vault.Material, target string, data []byte) (map[string][]byte, error) {
	var logical *vault.Material
	if data != nil {
		if IsLogicalSnapshot(data) {
			logical = vault.NewMaterial(data)
		} else {
			state, err := InspectSnapshot(data, target)
			if err != nil {
				return nil, err
			}
			p, err := convertLegacy(state, vaultID, master)
			if err != nil {
				return nil, err
			}
			var key *vault.Material
			if p.Sealed {
				key, err = vault.UnwrapKeyset(master, vaultID, p.ID, p.KeyEpoch, p.WrappedKey)
				if err != nil {
					return nil, err
				}
			}
			logical, err = p.ExportLogical(key)
			key.Destroy()
			if err != nil {
				return nil, err
			}
		}
		defer logical.Destroy()
	}
	return (vault.FileStore{Root: root, VaultID: vaultID, CatalogKey: master}).LogicalReplacementHeld(ctx, target, logical)
}
