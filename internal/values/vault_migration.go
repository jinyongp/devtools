package values

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/vault"
)

// LegacyInventory contains identifiers and an opaque revision, never values.
// The root/session key must be obtained before entering any maintenance gate.
type LegacyInventory struct {
	Profiles                 []string `json:"profiles"`
	SecretProfiles           []string `json:"secret_profiles"`
	LegacyReceiptCount       int      `json:"legacy_receipt_count"`
	LegacyBackupReceiptCount int      `json:"legacy_backup_receipt_count"`
	Fingerprint              string   `json:"fingerprint"`
}

type legacyMigrationState struct {
	inventory      LegacyInventory
	profiles       []*State
	mac            []byte
	previewKey     []byte
	backupReceipts map[string][]byte
}

func (s *legacyMigrationState) destroy() {
	clear(s.mac)
	clear(s.previewKey)
	s.previewKey = nil
	s.mac = nil
	s.profiles = nil
	for _, b := range s.backupReceipts {
		clear(b)
	}
	s.backupReceipts = nil
}

func inspectLegacyHeld(root string, master *vault.Material) (*legacyMigrationState, error) {
	if master == nil {
		return nil, vault.ErrLocked
	}
	directory := filepath.Join(root, "profiles")
	names, err := profilekey.Enumerate(directory, "values")
	if err != nil {
		return nil, vault.ErrRecovery
	}
	result := &legacyMigrationState{inventory: LegacyInventory{Profiles: names, SecretProfiles: []string{}}, backupReceipts: map[string][]byte{}}
	ok := false
	defer func() {
		if !ok {
			result.destroy()
		}
	}()
	k := master.CopyBytes()
	defer clear(k)
	if len(k) != 32 {
		return nil, vault.ErrKeyMissing
	}
	h := hmac.New(sha256.New, k)
	encoder := json.NewEncoder(h)
	if err := encoder.Encode("devtools-legacy-migration-v1"); err != nil {
		return nil, vault.ErrInvalid
	}
	totalBytes := 0
	for _, name := range names {
		b, err := profilekey.Snapshot(directory, name, "values", vault.MaxDocumentBytes)
		if err != nil {
			return nil, vault.ErrRecovery
		}
		totalBytes += len(b)
		if totalBytes > vault.MaxProtectedPlainBytes {
			clear(b)
			return nil, vault.ErrTooLarge
		}
		var state State
		err = vault.StrictDecode(b, &state)
		clear(b)
		if err != nil || !state.valid(name) {
			return nil, vault.ErrInvalid
		}
		if err = encoder.Encode(&state); err != nil {
			return nil, vault.ErrInvalid
		}
		for _, value := range state.Keys {
			if value.Kind == Secret {
				result.inventory.SecretProfiles = append(result.inventory.SecretProfiles, name)
				break
			}
		}
		result.profiles = append(result.profiles, &state)
	}
	result.mac, err = maintenance.Read(filepath.Join(root, ".maintenance", "values-key"), 32)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, vault.ErrRecovery
	}
	if err == nil && len(result.mac) != 32 {
		return nil, vault.ErrInvalid
	}
	if err := encoder.Encode(result.mac); err != nil {
		return nil, vault.ErrInvalid
	}
	result.previewKey, err = maintenance.Read(filepath.Join(root, ".maintenance", "preview-key"), 32)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, vault.ErrRecovery
	}
	if err == nil && len(result.previewKey) != 32 {
		return nil, vault.ErrInvalid
	}
	if err = encoder.Encode(result.previewKey); err != nil {
		return nil, vault.ErrInvalid
	}
	receipts, err := os.ReadDir(filepath.Join(root, "backup-receipts"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, vault.ErrRecovery
	}
	for _, entry := range receipts {
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil, vault.ErrRecovery
		}
		if !strings.HasPrefix(entry.Name(), "values-") {
			id := strings.TrimSuffix(entry.Name(), ".json")
			compact := strings.ToLower(strings.ReplaceAll(id, "-", ""))
			decoded, decodeErr := hex.DecodeString(compact)
			if len(id) != 36 || len(decoded) != 16 || decodeErr != nil || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || !strings.HasSuffix(entry.Name(), ".json") {
				return nil, vault.ErrInvalid
			}
			body, err := maintenance.Read(filepath.Join(root, "backup-receipts", entry.Name()), vault.MaxProtectedPlainBytes)
			if err != nil {
				return nil, vault.ErrRecovery
			}
			totalBytes += len(body)
			if totalBytes > vault.MaxProtectedPlainBytes {
				clear(body)
				return nil, vault.ErrTooLarge
			}
			var receipt struct {
				Fingerprint          string          `json:"fingerprint"`
				OperationFingerprint string          `json:"operation_fingerprint,omitempty"`
				Plan                 json.RawMessage `json:"plan"`
				Import               json.RawMessage `json:"import,omitempty"`
			}
			if vault.StrictDecode(body, &receipt) != nil || len(receipt.Fingerprint) != 64 || len(receipt.Plan) == 0 {
				clear(body)
				return nil, vault.ErrInvalid
			}
			if err = encoder.Encode(entry.Name()); err != nil {
				clear(body)
				return nil, vault.ErrInvalid
			}
			if err = encoder.Encode(body); err != nil {
				clear(body)
				return nil, vault.ErrInvalid
			}
			result.backupReceipts[entry.Name()] = body
			result.inventory.LegacyBackupReceiptCount++
			continue
		}
		body, err := maintenance.Read(filepath.Join(root, "backup-receipts", entry.Name()), vault.MaxDocumentBytes)
		if err != nil {
			return nil, vault.ErrRecovery
		}
		var receipt changeReceipt
		if vault.StrictDecode(body, &receipt) != nil || !project.ValidProfile(receipt.Result.Profile) || len(receipt.Fingerprint) != 64 || len(receipt.Result.Revision) != 64 {
			clear(body)
			return nil, vault.ErrInvalid
		}
		if _, err := hex.DecodeString(receipt.Fingerprint); err != nil {
			clear(body)
			return nil, vault.ErrInvalid
		}
		if err = encoder.Encode(entry.Name()); err != nil {
			return nil, vault.ErrInvalid
		}
		if err = encoder.Encode(body); err != nil {
			clear(body)
			return nil, vault.ErrInvalid
		}
		clear(body)
		result.inventory.LegacyReceiptCount++
	}
	if result.inventory.LegacyReceiptCount > 0 && len(result.mac) != 32 {
		return nil, vault.ErrKeyMissing
	}
	result.inventory.Fingerprint = hex.EncodeToString(h.Sum(nil))
	ok = true
	return result, nil
}

func InspectLegacyMigration(ctx context.Context, root string, master *vault.Material) (LegacyInventory, error) {
	release, err := maintenance.AcquireShared(ctx, root)
	if err != nil {
		return LegacyInventory{}, vault.ErrRecovery
	}
	defer release()
	if _, protected, err := maintenance.ProtectedRootHeld(root); err != nil || protected {
		return LegacyInventory{}, vault.ErrInvalid
	}
	state, err := inspectLegacyHeld(root, master)
	if err != nil {
		return LegacyInventory{}, err
	}
	defer state.destroy()
	return state.inventory, nil
}

// convertLegacy verifies the actual re-unwrapped key and every logical slot
// before any publication. Empty registrations and environment layers survive.
func convertLegacy(state *State, vaultID string, master *vault.Material) (*vault.Profile, error) {
	p, err := vault.NewProfile(state.Profile)
	if err != nil {
		return nil, err
	}
	var key *vault.Material
	for _, e := range state.Keys {
		if e.Kind == Secret {
			key, err = vault.NewKey()
			if err != nil {
				return nil, err
			}
			defer key.Destroy()
			p, err = vault.NewSealedProfile(state.Profile, vaultID, master, key)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	for _, env := range state.EnvNames() {
		if _, err = p.CreateEnv(env, key); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(state.Keys))
	for name := range state.Keys {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := state.Keys[name]
		kind := vault.ValueKind(e.Kind)
		if e.Common != nil {
			if _, err = p.Set(name, kind, "", *e.Common, key); err != nil {
				return nil, err
			}
		}
		envs := make([]string, 0, len(e.Envs))
		for env := range e.Envs {
			envs = append(envs, env)
		}
		sort.Strings(envs)
		for _, env := range envs {
			if _, err = p.Set(name, kind, env, e.Envs[env], key); err != nil {
				return nil, err
			}
		}
		if e.Common == nil && len(e.Envs) == 0 {
			id, err := vault.NewID()
			if err != nil {
				return nil, err
			}
			p.Values = append(p.Values, vault.Value{ID: id, Name: name, Kind: kind, Slots: []vault.Slot{}})
			if err = p.Sign(key); err != nil {
				return nil, err
			}
		}
	}
	if err = p.Sign(key); err != nil {
		return nil, err
	}
	var actual *vault.Material
	if p.Sealed {
		actual, err = vault.UnwrapKeyset(master, vaultID, p.ID, p.KeyEpoch, p.WrappedKey)
		if err != nil {
			return nil, err
		}
		defer actual.Destroy()
	}
	if err = p.Verify(actual); err != nil {
		return nil, err
	}
	// Verify each stored layer, rather than only effective envs that could hide
	// a common value under overrides.
	for _, v := range p.Values {
		e := state.Keys[v.Name]
		if e == nil || vault.ValueKind(e.Kind) != v.Kind {
			return nil, vault.ErrIntegrity
		}
		expected := len(e.Envs)
		if e.Common != nil {
			expected++
		}
		if len(v.Slots) != expected {
			return nil, vault.ErrIntegrity
		}
		for _, slot := range v.Slots {
			// Resolve a single layer in an isolated verified clone to prevent an
			// override from concealing an incorrect common slot.
			q := *p
			q.Values = []vault.Value{{ID: v.ID, Name: v.Name, Kind: v.Kind, Slots: []vault.Slot{slot}}}
			if err = q.Sign(actual); err != nil {
				return nil, err
			}
			got, _, err := q.Environment(slot.Env, actual, time.Now())
			if err != nil {
				return nil, err
			}
			want := e.Envs[slot.Env]
			if slot.Env == "" {
				if e.Common == nil {
					return nil, vault.ErrIntegrity
				}
				want = *e.Common
			}
			if got[v.Name] != want {
				return nil, vault.ErrIntegrity
			}
		}
	}
	return p, nil
}

// MigrateLegacy encrypts all legacy secret profiles in one root transaction.
// Callers pass the fingerprint returned by InspectLegacyMigration, so edits
// after preview fail before publication. Root-key creation/unwrap is external.
func MigrateLegacy(ctx context.Context, root, vaultID string, master *vault.Material, id, fingerprint string) (vault.ForwardResult, error) {
	forward := vault.ForwardStore{Root: root, VaultID: vaultID, Master: master}
	releaseLaunch, err := vault.AcquireLaunchMutation(ctx, filepath.Join(root, "vault"))
	if err != nil {
		return vault.ForwardResult{}, err
	}
	defer releaseLaunch()
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		return vault.ForwardResult{}, vault.ErrRecovery
	}
	defer release()
	if _, protected, err := maintenance.ProtectedRootHeld(root); err != nil {
		return vault.ForwardResult{}, err
	} else if protected {
		return forward.ReplaceHeld(ctx, id, fingerprint, map[string][]byte{"profiles/vault-catalog.json": nil})
	}
	legacy, err := inspectLegacyHeld(root, master)
	if err != nil {
		return vault.ForwardResult{}, err
	}
	defer legacy.destroy()
	if !hmac.Equal([]byte(fingerprint), []byte(legacy.inventory.Fingerprint)) {
		return vault.ForwardResult{}, errors.New("revision_conflict")
	}
	after := map[string][]byte{}
	catalog := vault.Catalog{Format: vault.StorageVersion, VaultID: vaultID, Entries: []vault.CatalogEntry{}}
	for _, state := range legacy.profiles {
		p, err := convertLegacy(state, vaultID, master)
		if err != nil {
			return vault.ForwardResult{}, err
		}
		b, err := p.Encode()
		if err != nil {
			return vault.ForwardResult{}, err
		}
		replacement, err := profilekey.Replacement(filepath.Join(root, "profiles"), state.Profile, "values", b)
		if err != nil {
			return vault.ForwardResult{}, vault.ErrRecovery
		}
		for name, data := range replacement {
			after[name] = data
		}
		if p.Sealed {
			catalog.Entries = append(catalog.Entries, vault.CatalogEntry{Name: p.Name, ID: p.ID, Generation: p.Generation, Revision: p.Revision, KeyEpoch: p.KeyEpoch, Sealed: true, Tag: append([]byte(nil), p.Tag...)})
		}
	}
	if err = catalog.Sign(master); err != nil {
		return vault.ForwardResult{}, err
	}
	b, err := json.Marshal(catalog)
	if err != nil {
		return vault.ForwardResult{}, err
	}
	after["profiles/vault-catalog.json"] = b
	if len(legacy.mac) > 0 {
		plain := vault.NewMaterial(legacy.mac)
		defer plain.Destroy()
		encrypted, err := vault.SealProtected(ctx, master, vaultID, vaultID, vault.MaterialLegacyVerification, plain)
		if err != nil {
			return vault.ForwardResult{}, err
		}
		actual, err := vault.OpenProtected(ctx, master, vaultID, vaultID, vault.MaterialLegacyVerification, encrypted)
		if err != nil {
			return vault.ForwardResult{}, err
		}
		got := actual.CopyBytes()
		actual.Destroy()
		same := bytes.Equal(got, legacy.mac)
		clear(got)
		if !same {
			return vault.ForwardResult{}, vault.ErrIntegrity
		}
		after["vault/legacy-verification.json"] = encrypted
		after[".maintenance/values-key"] = []byte(`{"vault_material":1}`)
	}
	// Legacy backup preview keys are not needed for replay: the committed
	// receipt is retained under root authentication. The encrypted recovery
	// archive captures this managed key before its raw file is removed.
	if len(legacy.previewKey) > 0 {
		after[".maintenance/preview-key"] = nil
	}
	for name, body := range legacy.backupReceipts {
		scope := strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(name, ".json"), "-", ""))
		plain := vault.NewMaterial(body)
		encrypted, err := vault.SealProtected(ctx, master, vaultID, scope, vault.MaterialBackupReceipt, plain)
		plain.Destroy()
		if err != nil {
			return vault.ForwardResult{}, err
		}
		after["backup-receipts/"+name] = encrypted
	}
	return forward.ReplaceHeld(ctx, id, fingerprint, after)
}
