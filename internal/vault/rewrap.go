package vault

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

const rewrapPending = "rewrap-pending.json"

type rewrapEnvelope struct {
	Format      int    `json:"rewrap_format"`
	ID          string `json:"operation_id"`
	VaultID     string `json:"vault_id"`
	Before      string `json:"before"`
	After       string `json:"after"`
	FromBackend string `json:"from_backend,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Protected   []byte `json:"protected"`
}
type rewrapState struct {
	ID     string  `json:"operation_id"`
	Before KeyFile `json:"before"`
	After  KeyFile `json:"after"`
}
type RewrapResult struct {
	Changed  bool   `json:"changed"`
	Replayed bool   `json:"replayed"`
	Backend  string `json:"backend"`
}

// RewrapInfo is routing metadata, not proof of authenticated intent. The actual
// operation authenticates its encrypted state and both slot identities again.
type RewrapInfo struct {
	RequestID         string `json:"request_id"`
	FromBackend       string `json:"from_backend,omitempty"`
	Backend           string `json:"backend,omitempty"`
	SlotPublished     bool   `json:"slot_published"`
	Completed         bool   `json:"completed"`
	IntegrityVerified bool   `json:"integrity_verified"`
}

func rewrapInfo(e *rewrapEnvelope, current *KeyFile, completed bool) (*RewrapInfo, error) {
	if e.VaultID != current.VaultID {
		return nil, ErrIntegrity
	}
	digest := keyFileDigest(current)
	if !completed && digest != e.Before && digest != e.After {
		return nil, ErrRecovery
	}
	id := e.ID
	requestID := id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	return &RewrapInfo{RequestID: requestID, FromBackend: e.FromBackend, Backend: e.Backend, SlotPublished: digest == e.After, Completed: completed}, nil
}

// An empty ID inspects only pending work. A supplied ID also finds its receipt,
// letting callers omit retired old credentials after the slot was published.
func (s KeyFileStore) RewrapInfo(id string) (*RewrapInfo, error) {
	if id != "" && !validID(id) {
		return nil, ErrInvalid
	}
	current, err := s.Read()
	if err != nil {
		return nil, err
	}
	r, err := platformfs.OpenRoot(s.Directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	defer r.Close()
	pending, err := readRewrap(r, rewrapPending)
	if err == nil {
		if id != "" && pending.ID != id {
			return nil, ErrRecovery
		}
		return rewrapInfo(pending, current, false)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRecovery
	}
	if id == "" {
		return nil, nil
	}
	receipt, err := readRewrap(r, rewrapReceipt(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return rewrapInfo(receipt, current, true)
}

func keyFileBytes(k *KeyFile) []byte { b, _ := json.Marshal(k); return b }
func keyFileDigest(k *KeyFile) string {
	d := sha256.Sum256(keyFileBytes(k))
	return hex.EncodeToString(d[:])
}
func protectorMatches(p Protector, k *KeyFile) bool {
	return p != nil && p.Identity() == (ProtectionIdentity{Backend: k.Backend, VaultID: k.VaultID})
}
func unwrapRoot(ctx context.Context, p Protector, k *KeyFile) (*Material, error) {
	if !protectorMatches(p, k) {
		return nil, ErrInvalid
	}
	b, err := p.Unwrap(ctx, k.Wrapped)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	if len(b) != 32 {
		return nil, ErrIntegrity
	}
	return NewMaterial(b), nil
}
func rewrapReceipt(id string) string { return "rewrap-" + id + ".json" }
func readRewrap(r *platformfs.Root, name string) (*rewrapEnvelope, error) {
	b, err := r.ReadFile(name, MaxKeySlotBytes*8)
	if err != nil {
		return nil, err
	}
	var e rewrapEnvelope
	if StrictDecode(b, &e) != nil || e.Format != 1 || !validID(e.ID) || !validID(e.VaultID) || len(e.Before) != 64 || len(e.After) != 64 || len(e.Protected) == 0 {
		return nil, ErrRecovery
	}
	for _, backend := range []string{e.FromBackend, e.Backend} {
		if backend != "" && backend != "passphrase" && backend != "keychain" && backend != "secret-service" && backend != "dpapi" {
			return nil, ErrRecovery
		}
	}
	return &e, nil
}
func openRewrap(ctx context.Context, e *rewrapEnvelope, master *Material) (*rewrapState, error) {
	p, err := OpenProtected(ctx, master, e.VaultID, e.ID, MaterialRewrap, e.Protected)
	if err != nil {
		return nil, err
	}
	defer p.Destroy()
	var s rewrapState
	if StrictDecode(p.data, &s) != nil || s.ID != e.ID || s.Before.validate() != nil || s.After.validate() != nil || s.Before.VaultID != e.VaultID || s.After.VaultID != e.VaultID || keyFileDigest(&s.Before) != e.Before || keyFileDigest(&s.After) != e.After {
		return nil, ErrIntegrity
	}
	if (e.FromBackend != "" && e.FromBackend != s.Before.Backend) || (e.Backend != "" && e.Backend != s.After.Backend) {
		return nil, ErrIntegrity
	}
	return &s, nil
}

// A native item may have been created just before process interruption, before
// its local intent was published. Reuse only that selected backend's exact vault
// reference, and only after authenticating that it contains the same root key.
// Never overwrite an existing native item or switch backend after an error.
func prepareRootProtection(ctx context.Context, p Protector, master *Material) ([]byte, error) {
	wrapped, err := p.Wrap(ctx, master.data)
	if err == nil {
		return wrapped, nil
	}
	if !errors.Is(err, ErrRecovery) {
		return nil, err
	}
	identity := p.Identity()
	var reference []byte
	switch identity.Backend {
	case "keychain":
		reference = []byte("keychain/v1/" + identity.VaultID)
	case "secret-service":
		reference = []byte("secret-service/v1/" + identity.VaultID)
	default:
		return nil, err
	}
	actual, readErr := p.Unwrap(ctx, reference)
	if readErr != nil {
		return nil, readErr
	}
	defer clear(actual)
	if !hmac.Equal(master.data, actual) {
		return nil, ErrIntegrity
	}
	return reference, nil
}

// Rewrap changes protection of the same random root key, not provider
// credentials or profile encryption keys. The operation is explicitly supplied
// both protectors; recovery selects exactly the protector of the current slot.
// Native calls are outside every maintenance/session/key-slot lock.
func (s KeyFileStore) Rewrap(ctx context.Context, id string, old, next Protector) (RewrapResult, error) {
	return s.rewrap(ctx, id, old, next, nil)
}
func (s KeyFileStore) rewrap(ctx context.Context, id string, old, next Protector, checkpoint func(string)) (RewrapResult, error) {
	step := func(stage string) {
		if checkpoint != nil {
			checkpoint(stage)
		}
	}
	result := RewrapResult{}
	if !validID(id) || old == nil || next == nil {
		return result, ErrInvalid
	}
	r, err := platformfs.OpenRoot(s.Directory, false)
	if err != nil {
		return result, ErrRecovery
	}
	defer r.Close()
	current, err := s.Read()
	if err != nil {
		return result, err
	}
	pending, err := readRewrap(r, rewrapPending)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, ErrRecovery
	}
	if pending != nil && pending.ID != id {
		return result, ErrRecovery
	}
	completed, receiptErr := readRewrap(r, rewrapReceipt(id))
	if receiptErr != nil && !errors.Is(receiptErr, os.ErrNotExist) {
		return result, ErrRecovery
	}
	if completed != nil {
		if pending != nil && (pending.ID != completed.ID || pending.Before != completed.Before || pending.After != completed.After || !hmac.Equal(pending.Protected, completed.Protected)) {
			return result, ErrRecovery
		}
		master, err := unwrapRoot(ctx, next, current)
		if err != nil {
			return result, err
		}
		defer master.Destroy()
		state, err := openRewrap(ctx, completed, master)
		if err != nil {
			return result, err
		}
		if !protectorMatches(old, &state.Before) || !protectorMatches(next, &state.After) {
			return result, ErrInvalid
		}
		// Check the requested new credentials against the committed slot even if a
		// later rewrap has happened. Never execute the old request again.
		verified, err := unwrapRoot(ctx, next, &state.After)
		if err != nil {
			return result, err
		}
		defer verified.Destroy()
		if !hmac.Equal(master.data, verified.data) {
			return result, ErrIntegrity
		}
		if pending != nil {
			release, err := maintenance.LockExclusive(ctx, filepath.Join(s.Directory, "key-slot-write.lock"))
			if err != nil {
				return result, err
			}
			observed, readErr := s.Read()
			if readErr != nil || keyFileDigest(observed) != completed.After {
				release()
				return result, ErrRecovery
			}
			err = r.RemoveFile(ctx, rewrapPending)
			release()
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return result, ErrRecovery
			}
		}
		return RewrapResult{Changed: completed.Before != completed.After, Replayed: true, Backend: state.After.Backend}, nil
	}
	var master *Material
	var state *rewrapState
	if pending != nil {
		if pending.VaultID != current.VaultID {
			return result, ErrIntegrity
		}
		selected := old
		switch keyFileDigest(current) {
		case pending.Before:
		case pending.After:
			selected = next
		default:
			return result, ErrRecovery
		}
		master, err = unwrapRoot(ctx, selected, current)
		if err != nil {
			return result, err
		}
		defer master.Destroy()
		state, err = openRewrap(ctx, pending, master)
		if err != nil {
			return result, err
		}
		if !protectorMatches(old, &state.Before) || !protectorMatches(next, &state.After) {
			return result, ErrInvalid
		}
		result.Replayed = true
	} else {
		if !protectorMatches(old, current) || next.Identity().VaultID != current.VaultID {
			return result, ErrInvalid
		}
		master, err = unwrapRoot(ctx, old, current)
		if err != nil {
			return result, err
		}
		defer master.Destroy()
		// Native item references are already bound to this vault/current account.
		// Re-selecting that same backend does not overwrite its only working item.
		after := *current
		if old.Identity() != next.Identity() || current.Backend == "passphrase" {
			after = KeyFile{Format: StorageVersion, VaultID: current.VaultID, Backend: next.Identity().Backend}
			after.Wrapped, err = prepareRootProtection(ctx, next, master)
			if err != nil {
				return result, err
			}
		}
		if err = after.validate(); err != nil {
			return result, err
		}
		actual, err := unwrapRoot(ctx, next, &after)
		if err != nil {
			return result, err
		}
		same := hmac.Equal(master.data, actual.data)
		actual.Destroy()
		if !same {
			return result, ErrIntegrity
		}
		state = &rewrapState{ID: id, Before: *current, After: after}
		plainBytes, _ := json.Marshal(state)
		plain := NewMaterial(plainBytes)
		clear(plainBytes)
		protected, err := SealProtected(ctx, master, current.VaultID, id, MaterialRewrap, plain)
		plain.Destroy()
		if err != nil {
			return result, err
		}
		pending = &rewrapEnvelope{Format: 1, ID: id, VaultID: current.VaultID, Before: keyFileDigest(current), After: keyFileDigest(&after), FromBackend: current.Backend, Backend: after.Backend, Protected: protected}
		release, err := maintenance.LockExclusive(ctx, filepath.Join(s.Directory, "key-slot-write.lock"))
		if err != nil {
			return result, err
		}
		observed, readErr := s.Read()
		if readErr != nil || keyFileDigest(observed) != pending.Before {
			release()
			return result, ErrInputChanged
		}
		body, _ := json.Marshal(pending)
		err = r.WriteFile(ctx, rewrapPending, body, false)
		release()
		if err != nil {
			return result, ErrRecovery
		}
	}
	step("prepared")
	// Re-check the actual durable recovery ciphertext before replacing the slot.
	durable, err := readRewrap(r, rewrapPending)
	if err != nil {
		return result, ErrRecovery
	}
	if durable.ID != id {
		return result, ErrRecovery
	}
	state, err = openRewrap(ctx, durable, master)
	if err != nil {
		return result, err
	}
	preparedKey, err := unwrapRoot(ctx, next, &state.After)
	if err != nil {
		return result, err
	}
	preparedSame := hmac.Equal(master.data, preparedKey.data)
	preparedKey.Destroy()
	if !preparedSame {
		return result, ErrIntegrity
	}
	// Preparation/native calls above do not hold any launch or storage lease.
	// Drain actual spawn admissions only for the slot publication boundary.
	finishLaunch, err := AcquireLaunchMutation(ctx, s.Directory)
	if err != nil {
		return result, err
	}
	release, err := maintenance.LockExclusive(ctx, filepath.Join(s.Directory, "key-slot-write.lock"))
	if err != nil {
		finishLaunch()
		return result, err
	}
	observed, readErr := s.Read()
	if readErr != nil {
		release()
		finishLaunch()
		return result, readErr
	}
	switch keyFileDigest(observed) {
	case durable.Before:
		err = r.WriteFile(ctx, keyFilename, keyFileBytes(&state.After), true)
	case durable.After:
	default:
		err = ErrInputChanged
	}
	release()
	finishLaunch()
	if err != nil {
		return result, err
	}
	step("slot-published")
	// Verify the installed slot through the selected new backend. Old native
	// material is retained if this fails, so explicit retry can finish recovery.
	installed, err := s.Read()
	if err != nil || keyFileDigest(installed) != durable.After {
		return result, ErrRecovery
	}
	actual, err := unwrapRoot(ctx, next, installed)
	if err != nil {
		return result, err
	}
	same := hmac.Equal(master.data, actual.data)
	actual.Destroy()
	if !same {
		return result, ErrIntegrity
	}
	step("installed-verified")
	if state.Before.Backend != state.After.Backend && (state.Before.Backend == "keychain" || state.Before.Backend == "secret-service") {
		remover, ok := old.(interface{ Remove(context.Context) error })
		if !ok {
			return result, ErrRecovery
		}
		if err = remover.Remove(ctx); err != nil && !errors.Is(err, ErrKeyMissing) {
			return result, err
		}
	}
	step("old-retired")
	release, err = maintenance.LockExclusive(ctx, filepath.Join(s.Directory, "key-slot-write.lock"))
	if err != nil {
		return result, err
	}
	defer release()
	observed, err = s.Read()
	if err != nil || keyFileDigest(observed) != durable.After {
		return result, ErrRecovery
	}
	body, _ := json.Marshal(durable)
	if err = r.WriteFile(ctx, rewrapReceipt(id), body, true); err != nil {
		return result, ErrRecovery
	}
	step("receipt-written")
	if err = r.RemoveFile(ctx, rewrapPending); err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, ErrRecovery
	}
	step("cleaned")
	return RewrapResult{Changed: durable.Before != durable.After, Replayed: result.Replayed, Backend: state.After.Backend}, nil
}
