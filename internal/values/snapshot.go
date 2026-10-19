package values

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

type SnapshotMetadata struct {
	Launch            *vault.LaunchTicket   `json:"launch,omitempty"`
	Identity          vault.InputIdentity   `json:"identity"`
	Slots             []vault.SlotReference `json:"slots"`
	IntegrityVerified bool                  `json:"integrity_verified"`
	EarliestExpiry    *time.Time            `json:"earliest_expiry,omitempty"`
}

// Snapshot is an owned, in-memory view of exactly one observed profile/env.
// State copies are explicit process/diagnostic boundaries, never disk writes.
type Snapshot struct {
	state     *State
	metadata  SnapshotMetadata
	canInject bool
}

func (Snapshot) MarshalJSON() ([]byte, error) {
	return nil, errors.New("value snapshots cannot be serialized")
}
func (Snapshot) MarshalText() ([]byte, error) {
	return nil, errors.New("value snapshots cannot be serialized")
}
func (Snapshot) Format(state fmt.State, _ rune) { io.WriteString(state, "[REDACTED]") }
func (s *State) Clone() *State {
	if s == nil {
		return nil
	}
	out := newState(s.Profile)
	out.Version = s.Version
	for env, exists := range s.Envs {
		out.Envs[env] = exists
	}
	for name, e := range s.Keys {
		if e == nil {
			out.Keys[name] = nil
			continue
		}
		copy := &entry{Kind: e.Kind, Envs: make(map[string]string, len(e.Envs))}
		if e.Common != nil {
			value := *e.Common
			copy.Common = &value
		}
		for env, value := range e.Envs {
			copy.Envs[env] = value
		}
		out.Keys[name] = copy
	}
	return out
}
func (s *Snapshot) StateCopy() *State {
	if s == nil {
		return nil
	}
	return s.state.Clone()
}
func (s *Snapshot) CanInject() bool { return s != nil && s.state != nil && s.canInject }
func (s *Snapshot) Metadata() SnapshotMetadata {
	if s == nil {
		return SnapshotMetadata{Slots: []vault.SlotReference{}}
	}
	m := s.metadata
	if m.Launch != nil {
		ticket := *m.Launch
		m.Launch = &ticket
	}
	m.Slots = append([]vault.SlotReference{}, m.Slots...)
	if m.EarliestExpiry != nil {
		expiry := *m.EarliestExpiry
		m.EarliestExpiry = &expiry
	}
	return m
}
func (s *Snapshot) Destroy() {
	if s == nil {
		return
	}
	if s.state != nil {
		for name, e := range s.state.Keys {
			e.Common = nil
			clear(e.Envs)
			delete(s.state.Keys, name)
		}
	}
	s.state = nil
}

func CaptureLegacy(state *State, env string, inject bool) (*Snapshot, *protocol.Error) {
	if state == nil || !state.valid(state.Profile) {
		return nil, storageError()
	}
	if err := state.CheckEnv(env); err != nil {
		return nil, err
	}
	id, err := vault.NewID()
	if err != nil {
		return nil, storageError()
	}
	s := &Snapshot{state: state.Clone(), canInject: inject, metadata: SnapshotMetadata{Identity: vault.InputIdentity{Revision: id}, Slots: []vault.SlotReference{}}}
	if !inject {
		for _, entry := range s.state.Keys {
			if entry.Kind != Secret {
				continue
			}
			if entry.Common != nil {
				empty := ""
				entry.Common = &empty
			}
			for env := range entry.Envs {
				entry.Envs[env] = ""
			}
		}
	}
	return s, nil
}

// CaptureVault obtains a root key once per snapshot, before the disk gate. All
// selected slots resolve from the one authenticated catalog/profile read.
func CaptureVault(ctx context.Context, root, profile, env string, inject bool, now time.Time, master func(context.Context) (*vault.Material, error)) (*Snapshot, *protocol.Error) {
	if master == nil {
		return nil, VaultError(ctx, vault.ErrLocked)
	}
	key, err := master(ctx)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	defer key.Destroy()
	k, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Read()
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	p, profileKey, err := (vault.FileStore{Root: root, VaultID: k.VaultID, CatalogKey: key}).ReadWithMaster(ctx, profile, key)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	defer profileKey.Destroy()
	effective, err := p.ResolveValues(env, profileKey, now, inject)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	s := &Snapshot{state: newState(profile), canInject: inject, metadata: SnapshotMetadata{Identity: vault.InputIdentity{ProfileID: p.ID, StorageGeneration: p.Generation}, IntegrityVerified: p.Sealed, Slots: []vault.SlotReference{}}}
	if env != "" {
		s.state.Envs[env] = true
	}
	for _, value := range effective {
		kind := Kind(value.Kind)
		if _, err := s.state.Set(kind, value.Reference.Name, value.Scope, value.Value); err != nil {
			s.Destroy()
			return nil, err
		}
		s.metadata.Slots = append(s.metadata.Slots, value.Reference)
		if (inject || kind == Variable) && value.ExpiresAt != nil {
			expires := time.Unix(*value.ExpiresAt, 0)
			if s.metadata.EarliestExpiry == nil || expires.Before(*s.metadata.EarliestExpiry) {
				s.metadata.EarliestExpiry = &expires
			}
		}
	}
	// This public revision hashes opaque metadata only, never value plaintext.
	e := vault.NewEncoder("effective-value-revision")
	e.String(p.ID)
	e.String(p.Generation)
	e.String(env)
	e.String("all")
	for _, slot := range s.metadata.Slots {
		e.String(slot.ValueID)
		e.String(slot.SlotID)
		e.String(slot.Version)
	}
	b, err := e.Finish()
	if err != nil {
		s.Destroy()
		return nil, VaultError(ctx, err)
	}
	digest := sha256.Sum256(b)
	s.metadata.Identity.Revision = hex.EncodeToString(digest[:])
	return s, nil
}

func VaultError(ctx context.Context, err error) *protocol.Error {
	if ctx.Err() != nil {
		return canceledError()
	}
	code := "storage_error"
	for _, known := range []error{vault.ErrLocked, vault.ErrKeyMissing, vault.ErrIntegrity, vault.ErrInvalid, vault.ErrTooLarge, vault.ErrUnavailable, vault.ErrRecovery, vault.ErrLockPending, vault.ErrDenied, vault.ErrInteractionRequired, vault.ErrBackendLocked, vault.ErrSessionProtocol} {
		if errors.Is(err, known) {
			code = known.Error()
			break
		}
	}
	if err != nil {
		switch err.Error() {
		case "value_expired", "env_not_found", "profile_not_found", "key_not_found", "kind_conflict":
			code = err.Error()
		}
	}
	return protocol.NewError(code, "Cannot prepare verified profile values.", 3, nil)
}

// CaptureVaultSession preserves the admission ticket from the same key delivery
// used to authenticate the input. A later unlocked session cannot refresh an old
// prepared input's authority silently.
func CaptureVaultSession(ctx context.Context, root, profile, env string, inject bool, now time.Time, client *vault.SessionClient) (*Snapshot, *protocol.Error) {
	var ticket *vault.LaunchTicket
	snapshot, err := CaptureVault(ctx, root, profile, env, inject, now, func(ctx context.Context) (*vault.Material, error) {
		key, t, e := client.UseWithTicket(ctx)
		ticket = t
		return key, e
	})
	if err != nil {
		return nil, err
	}
	if ticket != nil {
		copy := *ticket
		snapshot.metadata.Launch = &copy
	}
	return snapshot, nil
}
