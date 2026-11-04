package values

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/vault"
)

// StorageUpgradeAdapter keeps old-value decoding at the explicit migration
// boundary. Its borrowed key never starts or refreshes a session.
func StorageUpgradeAdapter(root string) storageupgrade.Adapter {
	adapter := storageupgrade.Layout("values", func(ctx context.Context, root string, step *storageupgrade.Step) error {
		raw, err := profilekey.Snapshot(filepath.Join(root, "profiles"), step.Profile, "values", vault.MaxDocumentBytes)
		if err != nil {
			return err
		}
		_, err = vault.DecodeProfile(raw)
		clear(raw)
		if err != nil {
			step.State, step.Reason = "blocked", "invalid_storage"
		}
		return nil
	})
	inspect := adapter.InspectHeld
	adapter.InspectHeld = func(ctx context.Context, root string) ([]storageupgrade.Step, error) {
		steps, err := inspect(ctx, root)
		if err != nil {
			return steps, err
		}
		receipts, err := inspectReceiptUpgrades(ctx, root)
		steps = append(steps, receipts...)
		if err != nil {
			if len(receipts) == 0 {
				steps = append(steps, (storageupgrade.Step{Domain: "values", Resource: "value_receipts"}).InspectionFailed(err))
			}
			return steps, err
		}
		_, protected, err := maintenance.ProtectedRootHeld(root)
		if err != nil {
			return append(steps, (storageupgrade.Step{Domain: "values", Resource: "root_protection"}).InspectionFailed(err)), err
		}
		slot, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Read()
		if errors.Is(err, vault.ErrKeyMissing) {
			if protected {
				steps = append(steps, storageupgrade.Step{Domain: "values", Source: "protected-root", Target: "protected-root", State: "blocked", Reason: "key_missing"})
			}
			return steps, nil
		}
		if err != nil {
			return append(steps, (storageupgrade.Step{Domain: "values", Resource: "vault_key"}).InspectionFailed(err)), err
		}
		if !protected {
			steps = append(steps, storageupgrade.Step{Domain: "values", Source: "unprotected-root", Target: "protected-root", State: "migration_required", Reason: "root_protection", Fingerprint: slot.VaultID})
		}
		return steps, nil
	}
	var master *vault.Material
	var vaultID string
	var identity [32]byte
	var expires *time.Time
	adapter.Authorize = func(ctx context.Context, _ []storageupgrade.Step) (func(), error) {
		store := vault.KeyFileStore{Directory: filepath.Join(root, "vault")}
		slot, err := store.Read()
		if errors.Is(err, vault.ErrKeyMissing) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		client, err := vault.OpenSessionClient(store.Directory)
		if err != nil {
			if errors.Is(err, vault.ErrUnavailable) {
				if _, endpointErr := os.Lstat(filepath.Join(store.Directory, "session.json")); errors.Is(endpointErr, os.ErrNotExist) {
					return nil, vault.ErrLocked
				}
			}
			return nil, err
		}
		master, err = client.InspectCached(ctx)
		if err != nil {
			return nil, err
		}
		status, err := client.Status(ctx)
		if err != nil {
			master.Destroy()
			master = nil
			return nil, err
		}
		if status.State != vault.Unlocked || status.IdleExpiresAt == nil {
			master.Destroy()
			master = nil
			return nil, vault.ErrLocked
		}
		expires = status.IdleExpiresAt
		vaultID = slot.VaultID
		identity, err = store.WriteIdentity()
		if err != nil {
			master.Destroy()
			master = nil
			return nil, err
		}
		return func() { master.Destroy(); master = nil }, nil
	}
	adapter.Apply = func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		if master != nil && expires != nil && !time.Now().Before(*expires) {
			return false, vault.ErrLocked
		}
		if master != nil && expires != nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, *expires)
			defer cancel()
		}
		if step.Profile == "" && step.Resource == "" {
			if master == nil {
				return false, vault.ErrLocked
			}
			inventory, err := InspectLegacyMigration(ctx, root, master)
			if err != nil {
				return false, err
			}
			id, err := vault.NewID()
			if err != nil {
				return false, err
			}
			_, err = migrateLegacy(ctx, root, vaultID, master, id, inventory.Fingerprint, &identity)
			return err == nil, err
		}
		directory := filepath.Join(root, "profiles")
		releaseLaunch, err := vault.AcquireLaunchMutation(ctx, filepath.Join(root, "vault"))
		if err != nil {
			return false, err
		}
		defer releaseLaunch()
		releaseSlot, err := maintenance.LockShared(ctx, filepath.Join(root, "vault", "key-slot-write.lock"))
		if err != nil {
			return false, err
		}
		defer releaseSlot()
		actualIdentity, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).WriteIdentity()
		if err != nil {
			return false, err
		}
		if actualIdentity != identity {
			return false, vault.ErrInputChanged
		}
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return false, err
		}
		defer release()
		if step.Resource != "" {
			return upgradeReceiptHeld(ctx, root, step, master, vaultID)
		}
		current, err := storageupgrade.ProfileLayout(directory, step.Profile, "values")
		if err != nil {
			return false, err
		}
		if current.State == "current" {
			return false, nil
		}
		if current.Fingerprint != step.Fingerprint {
			return false, vault.ErrInputChanged
		}
		raw, err := profilekey.MigrationSnapshot(directory, step.Profile, "values", vault.MaxDocumentBytes)
		if err != nil {
			return false, err
		}
		defer clear(raw)
		resolution, err := profilekey.Resolve(directory, step.Profile, "values")
		if err != nil {
			return false, err
		}
		path := resolution.CanonicalPath
		if resolution.Mode == profilekey.ModeLegacy {
			path = resolution.LegacyPath
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return false, err
		}
		p, err := vault.DecodeProfile(raw)
		if err != nil {
			legacy, err := InspectSnapshot(raw, step.Profile)
			if err != nil {
				return false, err
			}
			if hasSecret(legacy) && master == nil {
				return false, vault.ErrKeyMissing
			}
			p, err = convertLegacy(legacy, vaultID, master)
			if err != nil {
				return false, err
			}
		}
		var key *vault.Material
		if p.Sealed {
			if master == nil {
				return false, vault.ErrLocked
			}
			key, err = vault.UnwrapKeyset(master, p.VaultID, p.ID, p.KeyEpoch, p.WrappedKey)
			if err != nil {
				return false, err
			}
			defer key.Destroy()
		}
		if err := p.Verify(key); err != nil {
			return false, err
		}
		if master != nil {
			sum := sha256.Sum256([]byte("storage-upgrade/values/" + step.Profile + "/" + step.Fingerprint))
			fingerprint := hex.EncodeToString(sum[:])
			if err := (vault.FileStore{Root: root, VaultID: vaultID, CatalogKey: master}).PublishUpgradeHeld(ctx, p, key, fingerprint[:32], fingerprint); err != nil {
				return false, err
			}
		} else {
			if err := storageupgrade.RetainRecoveryHeld(root, "values", step.Profile, map[string][]byte{filepath.ToSlash(relative): raw}); err != nil {
				return false, err
			}
			if err := (ModernStore{Directory: directory, Profile: step.Profile}).publishHeld(ctx, p, key, nil, ""); err != nil {
				return false, err
			}
		}
		published, err := profilekey.Snapshot(directory, step.Profile, "values", vault.MaxDocumentBytes)
		if err != nil {
			return true, err
		}
		defer clear(published)
		verified, err := vault.DecodeProfile(published)
		if err != nil {
			return true, err
		}
		if err := verified.Verify(key); err != nil {
			return true, err
		}
		return true, nil
	}
	adapter.Recover = func(ctx context.Context, root string) (bool, error) {
		if master == nil {
			return false, VaultError(ctx, vault.ErrKeyMissing)
		}
		if expires != nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, *expires)
			defer cancel()
		}
		result, err := (vault.ForwardStore{Root: root, VaultID: vaultID, Master: master}).RecoverWithIdentity(ctx, identity)
		if err != nil {
			return false, VaultError(ctx, err)
		}
		return result.Committed, nil
	}
	authorize := adapter.Authorize
	inspectDomain := adapter.InspectHeld
	adapter.InspectHeld = func(ctx context.Context, root string) ([]storageupgrade.Step, error) {
		steps, err := inspectDomain(ctx, root)
		if err != nil {
			condition := VaultError(ctx, err)
			if len(steps) > 0 && steps[len(steps)-1].State == "blocked" {
				steps[len(steps)-1] = steps[len(steps)-1].InspectionFailed(condition)
			}
			return steps, condition
		}
		return steps, nil
	}
	adapter.Authorize = func(ctx context.Context, steps []storageupgrade.Step) (func(), error) {
		release, err := authorize(ctx, steps)
		if err != nil {
			return release, VaultError(ctx, err)
		}
		return release, nil
	}
	apply := adapter.Apply
	adapter.Apply = func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		changed, err := apply(ctx, root, step)
		if err != nil {
			return changed, VaultError(ctx, err)
		}
		return changed, nil
	}
	return adapter
}
