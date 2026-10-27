package values

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/vault"
)

// InspectRetentionHeld checks receipt metadata with existing replay semantics.
// The caller holds maintenance and, for protection, the inspection slot lease.
func PrepareReceiptInspection(ctx context.Context, root string) (*vault.Material, error) {
	key, _, err := sharedReceiptKey(ctx, root, "", nil, false)
	return key, err
}

func InspectRetentionHeld(ctx context.Context, inspection *vault.Inspection, publicKey *vault.Material, profile string, out *retention.Inventory) error {
	dir := filepath.Join(inspection.Root, "backup-receipts")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	seen := map[string]vault.ValueReceipt{}
	store := vault.FileStore{Root: inspection.Root, VaultID: inspection.VaultID, CatalogKey: inspection.Master}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "values-") {
			continue
		}
		if len(name) < len("values-")+41 || !strings.HasSuffix(name, ".json") {
			return vault.ErrInvalid
		}
		id := name[len(name)-41 : len(name)-5]
		if !modernRequestID.MatchString(id) {
			return vault.ErrInvalid
		}
		scope := strings.ToLower(strings.ReplaceAll(id, "-", ""))
		path := "backup-receipts/" + name
		r, found, err := store.ReadValueReceiptHeld(ctx, path, scope)
		if err != nil {
			return err
		}
		if !found {
			return vault.ErrInvalid
		}
		if inspection.Master == nil && publicKey == nil {
			return vault.ErrKeyMissing
		}
		var result ChangeResult
		if vault.StrictDecode(r.Result, &result) != nil || !project.ValidProfile(result.Profile) {
			return vault.ErrInvalid
		}
		keyPrefix := "values-" + profilekey.Key(result.Profile)
		legacyPrefix := "values-" + hex.EncodeToString([]byte(result.Profile))
		logical := valueRequestKey(id)
		if strings.HasPrefix(name, keyPrefix+"-s2-") {
			logical = strings.TrimSuffix(strings.TrimPrefix(name, keyPrefix+"-s2-"), "-"+id+".json")
			decoded, e := hex.DecodeString(logical)
			if e != nil || len(decoded) != 32 || logical[:32] != scope {
				return vault.ErrInvalid
			}
		} else if name != keyPrefix+"-"+id+".json" && name != legacyPrefix+"-"+id+".json" {
			return vault.ErrInvalid
		}
		logical = result.Profile + ":" + logical
		if previous, ok := seen[logical]; ok && !reflect.DeepEqual(previous, r) {
			return vault.ErrIntegrity
		}
		seen[logical] = r
		if profile != "" && profile != result.Profile {
			continue
		}
		info, err := platformfs.StatPrivate(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() {
			return vault.ErrInvalid
		}
		state := "replay_window"
		if r.ExpiresAt == 0 {
			state = "legacy_no_expiry"
		} else if time.Now().Unix() >= r.ExpiresAt {
			state = "spent"
		}
		verification := "public_metadata"
		if inspection.Master != nil {
			raw, err := maintenance.Read(filepath.Join(dir, name), vault.MaxDocumentBytes)
			if err != nil {
				return err
			}
			if strings.Contains(string(raw), `"protected_material_format"`) {
				verification = "authenticated"
			} else {
				verification = "legacy_metadata"
			}
		}
		out.Add("value_receipt", state, "profile", verification, "retain_request_identity", logical, false, info.Size())
	}
	return nil
}
