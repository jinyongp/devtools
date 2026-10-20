package values

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

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
	// WriteMaster also binds commit authority to the original session epoch.
	WriteMaster func(context.Context) (*vault.Material, func(context.Context) error, error)
	// Test seam after candidate computation, before commit lease acquisition.
	beforeCommit func()
	// Optional scoped measurement seam; never included in CLI responses.
	observe func(string, time.Duration)
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

func (s ModernStore) publishHeld(ctx context.Context, p *vault.Profile, key, master *vault.Material, vaultID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	if err = vault.AddCommitHint(files, p); err != nil {
		return err
	}
	return maintenance.Replace(s.root(), files)
}

var errUnchanged = errors.New("mutation unchanged")

func (s ModernStore) Update(ctx context.Context, change func(*Editor) (bool, *protocol.Error)) (bool, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return false, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}

	master, vaultID, validate, err := s.writeScope(ctx)
	if err != nil {
		return false, VaultError(ctx, err)
	}
	defer master.Destroy()
	p, key, base, err := s.readForEdit(ctx, master, vaultID)
	if err != nil {
		return false, VaultError(ctx, err)
	}
	defer key.Destroy()
	var editor *Editor
	defer func() {
		if editor != nil && editor.promoted {
			editor.key.Destroy()
		}
	}()
	requested := false
	stopPrepare := s.measure("prepare")
	changed, err := p.Batch(ctx, key, func(m *vault.Mutation) error {
		editor = newEditor(ctx, p, m, key, master, vaultID)
		var e *protocol.Error
		requested, e = change(editor)
		if e != nil {
			return e
		}
		if !requested {
			return errUnchanged
		}
		return nil
	})
	stopPrepare()
	if errors.Is(err, errUnchanged) {
		changed = false
		err = nil
	}
	if err != nil {
		if e, ok := err.(*protocol.Error); ok {
			return false, e
		}
		return false, VaultError(ctx, err)
	}
	promoted := changed && editor != nil && editor.promoted
	if promoted {
		key = editor.key
	}
	if s.beforeCommit != nil {
		s.beforeCommit()
	}
	release, err := s.lockCommit(ctx, base, promoted, validate)
	if err != nil {
		return false, VaultError(ctx, err)
	}
	defer release()
	current, currentKey, currentExists, err := s.readHeld(ctx, master, vaultID)
	if err != nil {
		return false, VaultError(ctx, err)
	}
	currentKey.Destroy()
	if e := s.checkBase(base, current, currentExists); e != nil {
		return false, e
	}
	if !changed {
		return false, nil
	}
	if err = s.publishHeld(ctx, p, key, master, vaultID); err != nil {
		return false, VaultError(ctx, err)
	}
	return true, nil
}
