package values

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// ModernStore owns the current logical profile format. Public-only roots need
// no key setup. Once the shared vault exists, authenticated writes acquire its
// reusable session key before disk locks; reads remain metadata/public only.
type ModernStore struct {
	Directory string
	Profile   string
	Master    func(context.Context) (*vault.Material, error)
}

func (s ModernStore) root() string { return maintenance.Root(s.Directory) }
func (s ModernStore) scope(ctx context.Context) (*vault.Material, string, error) {
	slot, err := (vault.KeyFileStore{Directory: filepath.Join(s.root(), "vault")}).Read()
	if errors.Is(err, vault.ErrKeyMissing) {
		if _, protected, e := maintenance.ProtectedRootHeld(s.root()); e != nil {
			return nil, "", vault.ErrRecovery
		} else if protected {
			return nil, "", vault.ErrKeyMissing
		}
		if _, e := os.Lstat(filepath.Join(s.Directory, "vault-catalog.json")); e == nil {
			return nil, "", vault.ErrKeyMissing
		} else if !errors.Is(e, os.ErrNotExist) {
			return nil, "", vault.ErrRecovery
		}
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if s.Master == nil {
		return nil, "", vault.ErrLocked
	}
	master, err := s.Master(ctx)
	if err != nil {
		return nil, "", err
	}
	raw := master.CopyBytes()
	valid := len(raw) == 32
	clear(raw)
	if !valid {
		master.Destroy()
		return nil, "", vault.ErrLocked
	}
	return master, slot.VaultID, nil
}

func (s ModernStore) readPublicHeld() (*vault.Profile, bool, error) {
	raw, err := profilekey.Snapshot(s.Directory, s.Profile, "values", vault.MaxDocumentBytes)
	if err != nil {
		return nil, false, vault.ErrRecovery
	}
	if raw == nil {
		p, err := vault.NewProfile(s.Profile)
		return p, false, err
	}
	defer clear(raw)
	p, err := vault.DecodeProfile(raw)
	if err == nil {
		return p, true, nil
	}
	// Only actual legacy disk state is eligible for public-format conversion.
	// Portable logical archives must never become plaintext profile files.
	if IsLogicalSnapshot(raw) {
		return nil, true, vault.ErrInvalid
	}
	legacy, err := InspectSnapshot(raw, s.Profile)
	if err != nil {
		return nil, true, vault.ErrInvalid
	}
	for _, entry := range legacy.Keys {
		if entry.Kind == Secret {
			return nil, true, vault.ErrKeyMissing
		}
	}
	p, err = convertLegacy(legacy, "", nil)
	return p, true, err
}
func (s ModernStore) readHeld(ctx context.Context, master *vault.Material, vaultID string) (*vault.Profile, *vault.Material, bool, error) {
	if master == nil {
		// Protection may have been initialized after scope() but before this
		// disk gate. Retry with an authenticated session instead of publishing
		// unsigned state or creating a raw receipt key in the protected root.
		if _, protected, err := maintenance.ProtectedRootHeld(s.root()); err != nil {
			return nil, nil, false, vault.ErrRecovery
		} else if protected {
			return nil, nil, false, vault.ErrInputChanged
		}
		if _, err := (vault.KeyFileStore{Directory: filepath.Join(s.root(), "vault")}).Read(); err == nil {
			return nil, nil, false, vault.ErrInputChanged
		} else if !errors.Is(err, vault.ErrKeyMissing) {
			return nil, nil, false, err
		}
		p, exists, err := s.readPublicHeld()
		if err != nil {
			return nil, nil, exists, err
		}
		if p.Sealed {
			return nil, nil, exists, vault.ErrKeyMissing
		}
		if err = p.Verify(nil); err != nil {
			return nil, nil, exists, err
		}
		return p, nil, exists, nil
	}
	p, key, err := (vault.FileStore{Root: s.root(), VaultID: vaultID, CatalogKey: master}).ReadAnyWithMasterHeld(ctx, s.Profile, master)
	if err != nil {
		if err.Error() != "profile_not_found" {
			return nil, nil, true, err
		}
		p, err = vault.NewProfile(s.Profile)
		return p, nil, false, err
	}
	return p, key, true, nil
}
func profileState(p *vault.Profile, key *vault.Material) (*State, error) {
	logical, err := p.ExportLogical(key)
	if err != nil {
		return nil, err
	}
	defer logical.Destroy()
	private, err := vault.LogicalLegacyView(logical, p.Name)
	if err != nil {
		return nil, err
	}
	defer private.Destroy()
	raw := private.CopyBytes()
	defer clear(raw)
	return InspectSnapshot(raw, p.Name)
}
func metadataState(p *vault.Profile) (*State, error) {
	// DecodeProfile validated structure. Secret plaintext is neither requested
	// from the session nor recovered for list/get/locked metadata responses.
	state := newState(p.Name)
	state.sealedUnverified = p.Sealed
	for _, env := range p.Envs {
		state.Envs[env] = true
	}
	state.expiries = map[string]map[string]int64{}
	for _, value := range p.Values {
		entry := &entry{Kind: Kind(value.Kind), Envs: map[string]string{}}
		for _, slot := range value.Slots {
			text := ""
			if value.Kind == vault.Public {
				text = *slot.PublicValue
			}
			if slot.Env == "" {
				entry.Common = &text
			} else {
				entry.Envs[slot.Env] = text
			}
			if slot.ExpiresAt != nil {
				if state.expiries[value.Name] == nil {
					state.expiries[value.Name] = map[string]int64{}
				}
				state.expiries[value.Name][slot.Env] = *slot.ExpiresAt
			}
		}
		state.Keys[value.Name] = entry
	}
	return state, nil
}
func (s ModernStore) Read(ctx context.Context) (*State, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}

	release, err := maintenance.AcquireShared(ctx, s.root())
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	defer release()
	return s.ReadHeld(ctx)
}
func (s ModernStore) ReadHeld(ctx context.Context) (*State, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}

	if err := ctx.Err(); err != nil {
		return nil, VaultError(ctx, err)
	}
	raw, err := profilekey.Snapshot(s.Directory, s.Profile, "values", vault.MaxDocumentBytes)
	if err != nil {
		return nil, storageError()
	}
	defer clear(raw)
	return metadataFromRaw(raw, s.Profile)
}
func metadataFromRaw(raw []byte, profile string) (*State, *protocol.Error) {
	ctx := context.Background()
	if raw == nil {
		return newState(profile), nil
	}
	p, err := vault.DecodeProfile(raw)
	if err == nil {
		if p.Name != profile {
			return nil, VaultError(ctx, vault.ErrIntegrity)
		}
		state, e := metadataState(p)
		if e != nil {
			return nil, VaultError(ctx, e)
		}
		return state, nil
	}
	if IsLogicalSnapshot(raw) {
		return nil, VaultError(ctx, vault.ErrInvalid)
	}
	state, err := InspectSnapshot(raw, profile)
	if err != nil {
		return nil, VaultError(ctx, vault.ErrInvalid)
	}
	for _, entry := range state.Keys {
		if entry.Kind != Secret {
			continue
		}
		state.sealedUnverified = true
		if entry.Common != nil {
			empty := ""
			entry.Common = &empty
		}
		for env := range entry.Envs {
			entry.Envs[env] = ""
		}
	}
	return state, nil
}
func hasSecret(state *State) bool {
	for _, entry := range state.Keys {
		if entry.Kind == Secret {
			return true
		}
	}
	return false
}

func applyProfileDiff(p *vault.Profile, key *vault.Material, before, after *State) error {
	for _, env := range after.EnvNames() {
		if !before.Envs[env] {
			if _, err := p.CreateEnv(env, key); err != nil {
				return err
			}
		}
	}
	names := make([]string, 0, len(after.Keys))
	for name := range after.Keys {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := after.Keys[name]
		old := before.Keys[name]
		if old != nil && old.Kind != entry.Kind {
			return vault.ErrInvalid
		}
		if old == nil && entry.Common == nil && len(entry.Envs) == 0 {
			if _, err := p.Register(name, vault.ValueKind(entry.Kind), key); err != nil {
				return err
			}
		}
		if entry.Common != nil {
			if old == nil || old.Common == nil || *old.Common != *entry.Common {
				if _, err := p.Set(name, vault.ValueKind(entry.Kind), "", *entry.Common, key); err != nil {
					return err
				}
			}
		} else if old != nil && old.Common != nil {
			if _, err := p.Unset(name, vault.ValueKind(entry.Kind), "", key); err != nil {
				return err
			}
		}
		envs := make([]string, 0, len(entry.Envs))
		for env := range entry.Envs {
			envs = append(envs, env)
		}
		sort.Strings(envs)
		for _, env := range envs {
			if old != nil {
				if previous, exists := old.Envs[env]; exists && previous == entry.Envs[env] {
					continue
				}
			}
			if _, err := p.Set(name, vault.ValueKind(entry.Kind), env, entry.Envs[env], key); err != nil {
				return err
			}
		}
		if old != nil {
			for env := range old.Envs {
				if _, exists := entry.Envs[env]; !exists {
					if _, err := p.Unset(name, vault.ValueKind(entry.Kind), env, key); err != nil {
						return err
					}
				}
			}
		}
	}
	for name := range before.Keys {
		if after.Keys[name] == nil {
			return vault.ErrInvalid
		}
	}
	for _, env := range before.EnvNames() {
		if !after.Envs[env] {
			if _, err := p.RemoveEnv(env, key); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s ModernStore) publishHeld(ctx context.Context, p *vault.Profile, key, master *vault.Material, vaultID string) error {
	if master != nil {
		return (vault.FileStore{Root: s.root(), VaultID: vaultID, CatalogKey: master}).PublishManagedHeld(ctx, p, key)
	}
	if p.Sealed {
		return vault.ErrLocked
	}
	data, err := p.Encode()
	if err != nil {
		return err
	}
	files, err := profilekey.Replacement(s.Directory, s.Profile, "values", data)
	if err != nil {
		return vault.ErrRecovery
	}
	return maintenance.Replace(s.root(), files)
}
func (s ModernStore) Update(ctx context.Context, change func(*State) (bool, *protocol.Error)) (bool, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return false, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}

	master, vaultID, err := s.scope(ctx)
	if err != nil {
		return false, VaultError(ctx, err)
	}
	defer master.Destroy()
	release, err := maintenance.AcquireExclusive(ctx, s.root())
	if err != nil {
		return false, VaultError(ctx, err)
	}
	held := true
	defer func() {
		if held {
			release()
		}
	}()
	p, key, existed, err := s.readHeld(ctx, master, vaultID)
	if err != nil {
		return false, VaultError(ctx, err)
	}
	defer func() { key.Destroy() }()
	before, err := profileState(p, key)
	if err != nil {
		return false, VaultError(ctx, err)
	}
	after := before.Clone()
	changed, changeErr := change(after)
	if changeErr != nil {
		return false, changeErr
	}
	if !after.valid(s.Profile) {
		return false, storageError()
	}
	if !changed {
		return false, nil
	}
	if !p.Sealed && hasSecret(after) {
		if master == nil {
			return false, VaultError(ctx, vault.ErrKeyMissing)
		}
		// Freeze the callback result exactly once, then establish the physical
		// promotion barrier outside disk maintenance. A concurrent writer conflicts.
		original, err := p.Encode()
		if err != nil {
			return false, VaultError(ctx, err)
		}
		release()
		held = false
		launchRelease, err := vault.AcquireLaunchMutation(ctx, filepath.Join(s.root(), "vault"))
		if err != nil {
			return false, VaultError(ctx, err)
		}
		defer launchRelease()
		release, err = maintenance.AcquireExclusive(ctx, s.root())
		if err != nil {
			return false, VaultError(ctx, err)
		}
		held = true
		current, currentKey, currentExists, err := s.readHeld(ctx, master, vaultID)
		if err != nil {
			return false, VaultError(ctx, err)
		}
		currentKey.Destroy()
		observed, err := current.Encode()
		if err != nil {
			return false, VaultError(ctx, err)
		}
		if existed != currentExists || existed && !bytes.Equal(original, observed) {
			return false, failure("revision_conflict", "Profile changed during protection setup; retry with current state.")
		}
		key, err = p.Seal(vaultID, master)
		if err != nil {
			return false, VaultError(ctx, err)
		}
	}
	if err = applyProfileDiff(p, key, before, after); err != nil {
		return false, VaultError(ctx, err)
	}
	if err = s.publishHeld(ctx, p, key, master, vaultID); err != nil {
		return false, VaultError(ctx, err)
	}
	return true, nil
}
