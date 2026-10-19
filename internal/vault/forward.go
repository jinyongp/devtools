package vault

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
)

const forwardVersion = 3
const forwardPending = ".maintenance/restore-pending.json"

type forwardPointer struct {
	Version int    `json:"version"`
	ID      string `json:"transaction_id"`
	Phase   string `json:"phase"`
	VaultID string `json:"vault_id"`
}
type forwardEntry struct {
	Path          string `json:"path"`
	BeforePresent bool   `json:"before_present"`
	Before        []byte `json:"before"`
	AfterPresent  bool   `json:"after_present"`
	After         []byte `json:"after"`
}
type forwardWire struct {
	Format      int            `json:"format"`
	VaultID     string         `json:"vault_id"`
	ID          string         `json:"operation_id"`
	Fingerprint string         `json:"fingerprint"`
	Entries     []forwardEntry `json:"entries"`
}
type forwardState struct{ wire forwardWire }

func (forwardState) Format(s fmt.State, _ rune) { io.WriteString(s, "[REDACTED]") }
func (forwardState) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private transition state cannot be serialized")
}
func (s *forwardState) destroy() {
	for _, entry := range s.wire.Entries {
		clear(entry.Before)
		clear(entry.After)
	}
	s.wire.Entries = nil
}

type forwardReceipt struct {
	Format      int    `json:"format"`
	ID          string `json:"operation_id"`
	Fingerprint string `json:"fingerprint"`
}

type ForwardResult struct {
	ID           string `json:"operation_id"`
	RecoveryPath string `json:"recovery_path"`
	Committed    bool   `json:"committed"`
	Replayed     bool   `json:"replayed"`
}

// ForwardStore is reserved for a plaintext-to-protected boundary. It never uses
// maintenance.Replace, never publishes a plaintext before-image, and never rolls
// back to plaintext after durable forward intent. Domain callers validate their
// after-images and obtain keys/admission maintenance leases before entering it.
type ForwardStore struct {
	Root       string
	VaultID    string
	Master     *Material
	checkpoint func(string)
}

func recoveryName(id string) string { return ".maintenance/values-recovery-" + id + ".json" }
func recoveryReceiptName(id string) string {
	return ".maintenance/values-recovery-" + id + ".done.json"
}
func validForwardFingerprint(fingerprint string) bool {
	b, err := hex.DecodeString(fingerprint)
	return err == nil && len(b) == 32 && len(fingerprint) == 64 && strings.ToLower(fingerprint) == fingerprint
}
func validForwardPath(path string) bool {
	if !filepath.IsLocal(path) || strings.Contains(path, "\\") {
		return false
	}
	if path == ".maintenance/values-key" || path == "vault/legacy-verification.json" || path == "profiles/vault-catalog.json" {
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) == 3 && parts[0] == "profiles" && parts[1] == ".identity" {
		return profilekey.ValidKey(strings.TrimSuffix(parts[2], ".json")) && strings.HasSuffix(parts[2], ".json")
	}
	if len(parts) != 2 || parts[0] != "profiles" && parts[0] != "backup-receipts" || !strings.HasSuffix(parts[1], ".json") || strings.Contains(parts[1], "..") {
		return false
	}
	if parts[0] == "backup-receipts" {
		return parts[1] != ".json"
	}
	base := strings.TrimSuffix(parts[1], ".json")
	if profilekey.ValidKey(base) {
		return true
	}
	_, ok := profilekey.DecodeLegacyFilename(parts[1])
	return ok
}
func (s ForwardStore) step(phase string) {
	if s.checkpoint != nil {
		s.checkpoint(phase)
	}
}
func (s ForwardStore) valid() bool {
	return filepath.IsAbs(s.Root) && validID(s.VaultID) && s.Master != nil
}
func readPrivateMaybe(root *platformfs.Root, name string) ([]byte, bool, error) {
	b, err := root.ReadFile(name, MaxDocumentBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, ErrRecovery
	}
	return b, true, nil
}
func (s ForwardStore) loadState(ctx context.Context, root *platformfs.Root, id string) (*forwardState, bool, error) {
	b, exists, err := readPrivateMaybe(root, recoveryName(id))
	if err != nil || !exists {
		return nil, exists, err
	}
	plain, err := OpenProtected(ctx, s.Master, s.VaultID, id, MaterialRecovery, b)
	if err != nil {
		return nil, true, err
	}
	defer plain.Destroy()
	state := &forwardState{}
	if StrictDecode(plain.data, &state.wire) != nil {
		state.destroy()
		return nil, true, ErrIntegrity
	}
	wire := state.wire
	if wire.Format != StorageVersion || wire.VaultID != s.VaultID || wire.ID != id || !validForwardFingerprint(wire.Fingerprint) || len(wire.Entries) == 0 {
		state.destroy()
		return nil, true, ErrIntegrity
	}
	previous := ""
	for _, entry := range wire.Entries {
		if !validForwardPath(entry.Path) || entry.Path <= previous || !entry.BeforePresent && entry.Before != nil || !entry.AfterPresent && entry.After != nil {
			state.destroy()
			return nil, true, ErrIntegrity
		}
		previous = entry.Path
	}
	return state, true, nil
}
func (s ForwardStore) receipt(ctx context.Context, root *platformfs.Root, id, fingerprint string) (bool, error) {
	b, exists, err := readPrivateMaybe(root, recoveryReceiptName(id))
	if err != nil || !exists {
		return false, err
	}
	plain, err := OpenProtected(ctx, s.Master, s.VaultID, id, MaterialReceipt, b)
	if err != nil {
		return false, err
	}
	defer plain.Destroy()
	var receipt forwardReceipt
	if StrictDecode(plain.data, &receipt) != nil || receipt.Format != StorageVersion || receipt.ID != id || !validForwardFingerprint(receipt.Fingerprint) {
		return false, ErrIntegrity
	}
	if subtle.ConstantTimeCompare([]byte(receipt.Fingerprint), []byte(fingerprint)) != 1 {
		return false, errorsNamed("request_conflict")
	}
	return true, nil
}
func (s ForwardStore) saveReceipt(ctx context.Context, root *platformfs.Root, state *forwardState) error {
	if found, err := s.receipt(ctx, root, state.wire.ID, state.wire.Fingerprint); err != nil || found {
		return err
	}
	b, _ := json.Marshal(forwardReceipt{Format: StorageVersion, ID: state.wire.ID, Fingerprint: state.wire.Fingerprint})
	plain := NewMaterial(b)
	clear(b)
	defer plain.Destroy()
	protected, err := SealProtected(ctx, s.Master, s.VaultID, state.wire.ID, MaterialReceipt, plain)
	if err != nil {
		return err
	}
	return root.WriteFile(ctx, recoveryReceiptName(state.wire.ID), protected, false)
}
func (s ForwardStore) apply(ctx context.Context, root *platformfs.Root, state *forwardState, pointer forwardPointer) error {
	if pointer.Phase == "applying" {
		for _, entry := range state.wire.Entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.AfterPresent {
				if err := root.MkdirAll(filepath.Dir(entry.Path)); err != nil {
					return ErrRecovery
				}
				if err := root.WriteFile(ctx, entry.Path, entry.After, true); err != nil {
					return ErrRecovery
				}
			} else if err := root.RemoveFile(ctx, entry.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return ErrRecovery
			}
			s.step("file:" + entry.Path)
		}
		if err := s.saveReceipt(ctx, root, state); err != nil {
			return err
		}
		s.step("receipt-written")
		pointer.Phase = "committed"
		body, _ := json.Marshal(pointer)
		if err := root.WriteFile(ctx, forwardPending, body, true); err != nil {
			return ErrRecovery
		}
		s.step("committed")
	} else {
		// A committed pointer may reappear after interrupted directory cleanup.
		// Never restore its old after-images over later legitimate mutations.
		if found, err := s.receipt(ctx, root, state.wire.ID, state.wire.Fingerprint); err != nil || !found {
			return ErrRecovery
		}
	}
	ready, _ := json.Marshal(forwardPointer{Version: forwardVersion, Phase: "ready", VaultID: s.VaultID})
	if err := root.WriteFile(ctx, forwardPending, ready, true); err != nil {
		return ErrRecovery
	}
	s.step("cleaned")
	return nil
}

// Replace's fingerprint must be a keyed, opaque logical request fingerprint,
// not a public plaintext hash. A retry reuses its authenticated frozen archive;
// newly generated ciphertext cannot reinterpret the same operation ID.
func (s ForwardStore) Replace(ctx context.Context, id, fingerprint string, after map[string][]byte) (ForwardResult, error) {
	result := ForwardResult{ID: id, RecoveryPath: filepath.Join(s.Root, recoveryName(id))}
	if !s.valid() || !validID(id) || !validForwardFingerprint(fingerprint) || len(after) == 0 {
		return result, ErrInvalid
	}
	releaseLaunch, err := AcquireLaunchMutation(ctx, filepath.Join(s.Root, "vault"))
	if err != nil {
		return result, err
	}
	defer releaseLaunch()
	release, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		return result, ErrRecovery
	}
	defer release()
	return s.ReplaceHeld(ctx, id, fingerprint, after)
}

// ReplaceHeld requires the exclusive launch mutation lease, then the exclusive
// data-root maintenance gate. The caller may
// inspect legacy files and prepare validated after-images under that same gate.
// Acquire native/session keys before entering it.
func (s ForwardStore) ReplaceHeld(ctx context.Context, id, fingerprint string, after map[string][]byte) (ForwardResult, error) {
	result := ForwardResult{ID: id, RecoveryPath: filepath.Join(s.Root, recoveryName(id))}
	if !s.valid() || !validID(id) || !validForwardFingerprint(fingerprint) || len(after) == 0 {
		return result, ErrInvalid
	}
	protectedID, protected, markerErr := maintenance.ProtectedRootHeld(s.Root)
	if markerErr != nil || protected && protectedID != s.VaultID {
		return result, ErrRecovery
	}
	root, err := platformfs.OpenRoot(s.Root, false)
	if err != nil {
		return result, ErrRecovery
	}
	defer root.Close()
	if committed, err := s.receipt(ctx, root, id, fingerprint); err != nil {
		return result, err
	} else if committed {
		result.Committed = true
		result.Replayed = true
		return result, nil
	}
	state, exists, err := s.loadState(ctx, root, id)
	if err != nil {
		return result, err
	}
	if exists {
		defer state.destroy()
		if state.wire.Fingerprint != fingerprint {
			return result, errorsNamed("request_conflict")
		}
		for _, entry := range state.wire.Entries {
			b, present, err := readPrivateMaybe(root, entry.Path)
			if err != nil {
				return result, err
			}
			same := present == entry.BeforePresent && bytes.Equal(b, entry.Before)
			clear(b)
			if !same {
				return result, errorsNamed("revision_conflict")
			}
		}
		result.Replayed = true
	} else {
		state = &forwardState{wire: forwardWire{Format: StorageVersion, VaultID: s.VaultID, ID: id, Fingerprint: fingerprint, Entries: []forwardEntry{}}}
		defer state.destroy()
		paths := make([]string, 0, len(after))
		for path := range after {
			if !validForwardPath(path) {
				return result, ErrInvalid
			}
			paths = append(paths, path)
		}
		sort.Strings(paths)
		estimatedBytes := int64(512)
		for _, path := range paths {
			b, present, err := readPrivateMaybe(root, path)
			if err != nil {
				return result, err
			}
			data := after[path]
			// Account for base64 and escaped metadata before allocating the
			// combined JSON recovery document. No oversized plaintext spool.
			estimatedBytes += int64(512+6*len(path)) + 4*((int64(len(b))+2)/3) + 4*((int64(len(data))+2)/3)
			if estimatedBytes > MaxProtectedPlainBytes {
				clear(b)
				return result, ErrTooLarge
			}
			state.wire.Entries = append(state.wire.Entries, forwardEntry{Path: path, BeforePresent: present, Before: b, AfterPresent: data != nil, After: append([]byte(nil), data...)})
		}
		plainBytes, err := json.Marshal(state.wire)
		if err != nil {
			return result, ErrInvalid
		}
		if len(plainBytes) > MaxProtectedPlainBytes {
			clear(plainBytes)
			return result, ErrTooLarge
		}
		plain := NewMaterial(plainBytes)
		clear(plainBytes)
		encrypted, err := SealProtected(ctx, s.Master, s.VaultID, id, MaterialRecovery, plain)
		plain.Destroy()
		if err != nil {
			return result, err
		}
		if err = root.WriteFile(ctx, recoveryName(id), encrypted, false); err != nil {
			return result, ErrRecovery
		}
		// Validate the actual durable ciphertext, not only the in-memory envelope.
		verified, found, err := s.loadState(ctx, root, id)
		if err != nil || !found {
			return result, ErrRecovery
		}
		verified.destroy()
		s.step("recovery-written")
	}
	pointer := forwardPointer{Version: forwardVersion, ID: id, Phase: "applying", VaultID: s.VaultID}
	body, _ := json.Marshal(pointer)
	if err = root.WriteFile(ctx, forwardPending, body, true); err != nil {
		return result, ErrRecovery
	}
	s.step("intent-published")
	if err = s.apply(ctx, root, state, pointer); err != nil {
		return result, err
	}
	result.Committed = true
	return result, nil
}

// Recover must be an explicit key-aware recovery path. Ordinary and older
// maintenance readers reject version 3 and cannot copy a plaintext rollback.
func (s ForwardStore) Recover(ctx context.Context) (ForwardResult, error) {
	var result ForwardResult
	if !s.valid() {
		return result, ErrInvalid
	}
	releaseLaunch, err := AcquireLaunchMutation(ctx, filepath.Join(s.Root, "vault"))
	if err != nil {
		return result, err
	}
	defer releaseLaunch()
	release, err := maintenance.AcquireForRecovery(ctx, s.Root)
	if err != nil {
		return result, ErrRecovery
	}
	defer release()
	root, err := platformfs.OpenRoot(s.Root, false)
	if err != nil {
		return result, ErrRecovery
	}
	defer root.Close()
	body, exists, err := readPrivateMaybe(root, forwardPending)
	if err != nil || !exists {
		return result, err
	}
	var pointer forwardPointer
	if StrictDecode(body, &pointer) == nil && pointer.Version == forwardVersion && pointer.VaultID == s.VaultID && pointer.Phase == "ready" && pointer.ID == "" {
		return result, nil
	}
	if StrictDecode(body, &pointer) != nil || pointer.Version != forwardVersion || !validID(pointer.ID) || pointer.VaultID != s.VaultID || pointer.Phase != "applying" && pointer.Phase != "committed" {
		return result, ErrRecovery
	}
	result = ForwardResult{ID: pointer.ID, RecoveryPath: filepath.Join(s.Root, recoveryName(pointer.ID)), Replayed: true}
	state, found, err := s.loadState(ctx, root, pointer.ID)
	if err != nil || !found {
		return result, ErrRecovery
	}
	defer state.destroy()
	if err = s.apply(ctx, root, state, pointer); err != nil {
		return result, err
	}
	result.Committed = true
	return result, nil
}
