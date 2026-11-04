package values

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/vault"
)

func legacyReceiptAddress(name string) (key, id string, ok bool) {
	if !strings.HasPrefix(name, "values-") || !strings.HasSuffix(name, ".json") {
		return "", "", false
	}
	stem := strings.TrimSuffix(strings.TrimPrefix(name, "values-"), ".json")
	if len(stem) < 38 {
		return "", "", false
	}
	id = stem[len(stem)-36:]
	if stem[len(stem)-37] != '-' || !modernRequestID.MatchString(id) {
		return "", "", false
	}
	key = stem[:len(stem)-37]
	if profilekey.ValidKey(key) {
		return key, id, true
	}
	raw, err := hex.DecodeString(key)
	if err != nil || !project.ValidProfile(string(raw)) {
		return "", "", false
	}
	return profilekey.Key(string(raw)), id, true
}

func currentReceiptAddress(name string) bool {
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, "values-"), ".json"), "-s2-")
	if len(parts) != 2 || !strings.HasSuffix(name, ".json") || !profilekey.ValidKey(parts[0]) || len(parts[1]) != 101 {
		return false
	}
	key := parts[1][:64]
	raw, err := hex.DecodeString(key)
	if err != nil || len(raw) != 32 || key != strings.ToLower(key) || parts[1][64] != '-' {
		return false
	}
	scope := key[:32]
	id := scope[:8] + "-" + scope[8:12] + "-" + scope[12:16] + "-" + scope[16:20] + "-" + scope[20:]
	return parts[1][65:] == id
}

func inspectReceiptUpgrades(ctx context.Context, root string) ([]storageupgrade.Step, error) {
	directory := filepath.Join(root, "backup-receipts")
	if err := platformfs.CheckDirectory(directory); errors.Is(err, os.ErrNotExist) {
		return []storageupgrade.Step{}, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	finish := progress.Begin(ctx, "Checking value operation records")
	defer finish()
	steps := []storageupgrade.Step{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "values-") {
			continue
		}
		current := currentReceiptAddress(name)
		step := storageupgrade.Step{Domain: "values", Resource: name, Source: "value-receipt-v1", Target: "value-receipt-s2", State: "migration_required"}
		if current {
			step.Source, step.State = "value-receipt-s2", "current"
		}
		if err := ctx.Err(); err != nil {
			return append(steps, step.InspectionFailed(err)), err
		}
		_, id, old := legacyReceiptAddress(name)
		if !current && !old {
			step.State, step.Reason = "unsupported", "unsupported_receipt_address"
			steps = append(steps, step)
			continue
		}
		raw, err := maintenance.Read(filepath.Join(directory, name), vault.MaxDocumentBytes)
		if err != nil {
			return append(steps, step.InspectionFailed(err)), err
		}
		if err := ctx.Err(); err != nil {
			clear(raw)
			return append(steps, step.InspectionFailed(err)), err
		}
		var header struct {
			Format  *int   `json:"protected_material_format"`
			VaultID string `json:"vault_id"`
		}
		if json.Unmarshal(raw, &header) != nil {
			step.State, step.Reason = "blocked", "invalid_storage"
		} else if header.Format != nil {
			scope := strings.ToLower(strings.ReplaceAll(id, "-", ""))
			if current {
				parts := strings.Split(name, "-s2-")
				scope = parts[1][:32]
			}
			if *header.Format != vault.StorageVersion {
				step.State, step.Reason = "unsupported", "unsupported_receipt_format"
			} else if vault.InspectProtected(raw, header.VaultID, scope, vault.MaterialValueReceipt) != nil {
				step.State, step.Reason = "blocked", "invalid_storage"
			}
		} else {
			var receipt vault.ValueReceipt
			var result ChangeResult
			if vault.StrictDecode(raw, &receipt) != nil || len(receipt.Fingerprint) != 64 || vault.StrictDecode(receipt.Result, &result) != nil || !project.ValidProfile(result.Profile) || receipt.KeyEpoch != 0 {
				step.State, step.Reason = "blocked", "invalid_storage"
			}
		}
		sum := sha256.Sum256(raw)
		clear(raw)
		if err := ctx.Err(); err != nil {
			return append(steps, step.InspectionFailed(err)), err
		}
		step.Fingerprint = hex.EncodeToString(sum[:])
		if step.State != "current" {
			steps = append(steps, step)
		}
	}
	return steps, nil
}

func upgradeReceiptHeld(ctx context.Context, root string, step storageupgrade.Step, master *vault.Material, vaultID string) (bool, error) {
	key, id, ok := legacyReceiptAddress(step.Resource)
	if !ok || filepath.Base(step.Resource) != step.Resource {
		return false, vault.ErrInvalid
	}
	source := "backup-receipts/" + step.Resource
	target := currentReceiptRelative(key, id)
	raw, err := maintenance.Read(filepath.Join(root, source), vault.MaxDocumentBytes)
	if errors.Is(err, os.ErrNotExist) {
		_, found, err := (vault.FileStore{Root: root, VaultID: vaultID, CatalogKey: master}).ReadValueReceiptHeld(ctx, target, valueReceiptScope(id))
		if err == nil && !found {
			err = vault.ErrInputChanged
		}
		return false, err
	}
	if err != nil {
		return false, err
	}
	defer clear(raw)
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != step.Fingerprint {
		return false, vault.ErrInputChanged
	}
	store := vault.FileStore{Root: root, VaultID: vaultID, CatalogKey: master}
	receipt, found, err := store.ReadValueReceiptHeld(ctx, source, strings.ToLower(strings.ReplaceAll(id, "-", "")))
	if err != nil {
		return false, err
	}
	if !found {
		return false, vault.ErrInputChanged
	}
	var result ChangeResult
	if vault.StrictDecode(receipt.Result, &result) != nil || !project.ValidProfile(result.Profile) || profilekey.Key(result.Profile) != key {
		return false, vault.ErrInvalid
	}
	if receipt.KeyEpoch == 0 {
		verify, _, err := sharedReceiptKey(ctx, root, vaultID, master, false)
		if err != nil {
			return false, err
		}
		verify.Destroy()
	}
	previous, present, err := store.ReadValueReceiptHeld(ctx, target, valueReceiptScope(id))
	if err != nil {
		return false, err
	}
	if present && !reflect.DeepEqual(receipt, previous) {
		return false, vault.ErrIntegrity
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		return false, err
	}
	defer clear(body)
	if master != nil {
		plain := vault.NewMaterial(body)
		body, err = vault.SealProtected(ctx, master, vaultID, valueReceiptScope(id), vault.MaterialValueReceipt, plain)
		plain.Destroy()
		if err != nil {
			return false, err
		}
	}
	files := map[string][]byte{source: nil, target: body}
	if master != nil {
		fingerprint := sha256.Sum256([]byte("storage-upgrade/receipt/" + step.Resource + "/" + step.Fingerprint))
		text := hex.EncodeToString(fingerprint[:])
		_, err = (vault.ForwardStore{Root: root, VaultID: vaultID, Master: master}).ReplaceHeld(ctx, text[:32], text, files)
	} else {
		if err = storageupgrade.RetainRecoveryHeld(root, "values", result.Profile, map[string][]byte{source: raw}); err == nil {
			err = maintenance.Replace(root, files)
		}
	}
	if err != nil {
		return false, err
	}
	verified, present, err := store.ReadValueReceiptHeld(ctx, target, valueReceiptScope(id))
	if err != nil {
		return true, err
	}
	if !present || !reflect.DeepEqual(verified, receipt) {
		return true, vault.ErrIntegrity
	}
	return true, nil
}
