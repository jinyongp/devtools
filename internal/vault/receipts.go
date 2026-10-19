package vault

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
)

// ValueReceipt contains request MAC and public result metadata only. Secret
// request bodies never enter receipts. New sealed receipts are root-protected.
type ValueReceipt struct {
	Fingerprint string          `json:"fingerprint"`
	Result      json.RawMessage `json:"result"`
	ProfileID   string          `json:"profile_id,omitempty"`
	KeyEpoch    uint64          `json:"key_epoch,omitempty"`
	CreatedAt   int64           `json:"created_at,omitempty"`
	ExpiresAt   int64           `json:"expires_at,omitempty"`
}
type receiptEpoch struct {
	Epoch uint64 `json:"epoch"`
	Key   []byte `json:"key"`
}
type receiptHistory struct {
	ProfileID string         `json:"profile_id"`
	Profile   string         `json:"profile"`
	Keys      []receiptEpoch `json:"keys"`
}

func (h *receiptHistory) destroy() {
	for _, entry := range h.Keys {
		clear(entry.Key)
	}
	h.Keys = nil
}
func historyPath(id string) string { return "vault/receipt-keys-" + id + ".json" }
func ReceiptKey(profileKey *Material, vaultID, profileID string) (*Material, error) {
	if !validID(vaultID) || !validID(profileID) {
		return nil, ErrInvalid
	}
	key, err := derive(profileKey, vaultID, profileID, "request-fingerprint")
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return NewMaterial(key), nil
}
func receiptID(name string) (string, bool) {
	if !strings.HasSuffix(name, ".json") || len(name) < 41 {
		return "", false
	}
	id := strings.TrimSuffix(name, ".json")
	id = id[len(id)-36:]
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return "", false
	}
	compact := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	return compact, validID(compact)
}
func (s FileStore) ReadValueReceiptHeld(ctx context.Context, path, id string) (ValueReceipt, bool, error) {
	var receipt ValueReceipt
	raw, err := maintenance.Read(filepath.Join(s.Root, path), MaxDocumentBytes)
	if errors.Is(err, os.ErrNotExist) {
		return receipt, false, nil
	}
	if err != nil {
		return receipt, false, ErrRecovery
	}
	protectedReceipt := false
	var header map[string]json.RawMessage
	if json.Unmarshal(raw, &header) != nil {
		return receipt, false, ErrInvalid
	}
	if _, protected := header["protected_material_format"]; protected {
		protectedReceipt = true
		material, err := OpenProtected(ctx, s.CatalogKey, s.VaultID, id, MaterialValueReceipt, raw)
		if err != nil {
			return receipt, false, err
		}
		defer material.Destroy()
		raw = material.CopyBytes()
		defer clear(raw)
	}
	if StrictDecode(raw, &receipt) != nil || len(receipt.Fingerprint) != 64 || len(receipt.Result) == 0 {
		return receipt, false, ErrInvalid
	}
	if receipt.ProfileID != "" && !validID(receipt.ProfileID) {
		return receipt, false, ErrInvalid
	}
	if receipt.KeyEpoch > 0 && !protectedReceipt {
		return receipt, false, ErrIntegrity
	}
	if receipt.KeyEpoch > 0 && receipt.ProfileID == "" {
		return receipt, false, ErrInvalid
	}
	if receipt.CreatedAt != 0 && (receipt.ExpiresAt <= receipt.CreatedAt) {
		return receipt, false, ErrInvalid
	}
	return receipt, true, nil
}
func (s FileStore) readHistory(ctx context.Context, id string) (*receiptHistory, bool, error) {
	raw, err := maintenance.Read(filepath.Join(s.Root, historyPath(id)), MaxDocumentBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, ErrRecovery
	}
	material, err := OpenProtected(ctx, s.CatalogKey, s.VaultID, id, MaterialReceiptHistory, raw)
	if err != nil {
		return nil, true, err
	}
	defer material.Destroy()
	history := &receiptHistory{}
	if StrictDecode(material.data, history) != nil || history.ProfileID != id || !namePattern.MatchString(history.Profile) || len(history.Keys) > 4096 {
		history.destroy()
		return nil, true, ErrInvalid
	}
	previous := uint64(0)
	for _, entry := range history.Keys {
		if entry.Epoch <= previous || len(entry.Key) != 32 {
			history.destroy()
			return nil, true, ErrInvalid
		}
		previous = entry.Epoch
	}
	return history, true, nil
}
func (s FileStore) HistoricalReceiptKeyHeld(ctx context.Context, profileID string, epoch uint64) (*Material, error) {
	if !validID(profileID) || epoch == 0 {
		return nil, ErrInvalid
	}
	history, found, err := s.readHistory(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrKeyMissing
	}
	defer history.destroy()
	for _, entry := range history.Keys {
		if entry.Epoch == epoch {
			return NewMaterial(entry.Key), nil
		}
	}
	return nil, ErrKeyMissing
}

func (s FileStore) liveReceiptEpochs(ctx context.Context, profileID, name string, now time.Time) (map[uint64]bool, error) {
	live := map[uint64]bool{}
	entries, err := os.ReadDir(filepath.Join(s.Root, "backup-receipts"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRecovery
	}
	prefix := "values-" + profilekey.Key(name) + "-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		id, ok := receiptID(entry.Name())
		if !ok || entry.IsDir() {
			return nil, ErrInvalid
		}
		receipt, found, err := s.ReadValueReceiptHeld(ctx, "backup-receipts/"+entry.Name(), id)
		if err != nil {
			return nil, err
		}
		if found && receipt.ProfileID == profileID && receipt.KeyEpoch > 0 && (receipt.ExpiresAt == 0 || now.Unix() < receipt.ExpiresAt) {
			live[receipt.KeyEpoch] = true
		}
	}
	return live, nil
}

// receiptHistoryFiles retains only derived request-verification keys needed by
// live receipts. The old data-decryption key is never stored. It is published
// atomically with the rotation/replacement that retires that data key.
func (s FileStore) receiptHistoryFiles(ctx context.Context, p *Profile, key *Material, now time.Time) (map[string][]byte, error) {
	live, err := s.liveReceiptEpochs(ctx, p.ID, p.Name, now)
	if err != nil {
		return nil, err
	}
	for epoch := range live {
		if epoch > p.KeyEpoch {
			return nil, ErrIntegrity
		}
	}
	history, found, err := s.readHistory(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		history = &receiptHistory{ProfileID: p.ID, Profile: p.Name, Keys: []receiptEpoch{}}
	}
	defer history.destroy()
	if history.Profile != p.Name {
		return nil, ErrIntegrity
	}
	retained := make([]receiptEpoch, 0, len(history.Keys)+1)
	for _, entry := range history.Keys {
		if live[entry.Epoch] {
			retained = append(retained, entry)
		} else {
			clear(entry.Key)
		}
	}
	history.Keys = retained
	if live[p.KeyEpoch] {
		fingerprintKey, err := ReceiptKey(key, p.VaultID, p.ID)
		if err != nil {
			return nil, err
		}
		already := false
		for _, entry := range history.Keys {
			if entry.Epoch == p.KeyEpoch {
				already = true
			}
		}
		if !already {
			history.Keys = append(history.Keys, receiptEpoch{Epoch: p.KeyEpoch, Key: fingerprintKey.CopyBytes()})
		}
		fingerprintKey.Destroy()
	}
	if len(history.Keys) == 0 {
		if found {
			return map[string][]byte{historyPath(p.ID): nil}, nil
		}
		return map[string][]byte{}, nil
	}
	if len(history.Keys) > 4096 {
		return nil, ErrTooLarge
	}
	sort.Slice(history.Keys, func(i, j int) bool { return history.Keys[i].Epoch < history.Keys[j].Epoch })
	raw, err := json.Marshal(history)
	if err != nil {
		return nil, ErrInvalid
	}
	plain := NewMaterial(raw)
	clear(raw)
	defer plain.Destroy()
	encrypted, err := SealProtected(ctx, s.CatalogKey, s.VaultID, p.ID, MaterialReceiptHistory, plain)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{historyPath(p.ID): encrypted}, nil
}

// PruneReceiptKeysHeld retires request-verification keys once no unexpired
// receipt needs them. Callers publish the returned encrypted changes in their
// maintenance transaction; no data-decryption keys are loaded or retained.
func (s FileStore) PruneReceiptKeysHeld(ctx context.Context, now time.Time) (map[string][]byte, error) {
	files := map[string][]byte{}
	entries, err := os.ReadDir(filepath.Join(s.Root, "vault"))
	if err != nil {
		return nil, ErrRecovery
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "receipt-keys-") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "receipt-keys-"), ".json")
		if entry.IsDir() || name != "receipt-keys-"+id+".json" || !validID(id) {
			return nil, ErrInvalid
		}
		history, found, err := s.readHistory(ctx, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, ErrRecovery
		}
		live, err := s.liveReceiptEpochs(ctx, id, history.Profile, now)
		if err != nil {
			history.destroy()
			return nil, err
		}
		kept := make([]receiptEpoch, 0, len(history.Keys))
		for _, key := range history.Keys {
			if live[key.Epoch] {
				kept = append(kept, key)
			} else {
				clear(key.Key)
			}
		}
		changed := len(kept) != len(history.Keys)
		history.Keys = kept
		if !changed {
			history.destroy()
			continue
		}
		if len(kept) == 0 {
			files[historyPath(id)] = nil
			history.destroy()
			continue
		}
		raw, err := json.Marshal(history)
		history.destroy()
		if err != nil {
			return nil, ErrInvalid
		}
		plain := NewMaterial(raw)
		clear(raw)
		encrypted, err := SealProtected(ctx, s.CatalogKey, s.VaultID, id, MaterialReceiptHistory, plain)
		plain.Destroy()
		if err != nil {
			return nil, err
		}
		files[historyPath(id)] = encrypted
	}
	return files, nil
}
