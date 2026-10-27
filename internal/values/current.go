package values

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// SessionMaster obtains the shared session before storage gates. Writes also
// revalidate their original session epoch before acquiring the commit gate.
func SessionMaster(root string) func(context.Context) (*vault.Material, error) {
	return func(ctx context.Context) (*vault.Material, error) {
		client, err := (vault.SessionManager{Directory: filepath.Join(root, "vault")}).Client(ctx)
		if err != nil {
			return nil, err
		}
		return client.Use(ctx)
	}
}
func CurrentStore(directory, profile string) ModernStore {
	root := maintenance.Root(directory)
	return ModernStore{Directory: directory, Profile: profile, Master: SessionMaster(root), WriteMaster: func(ctx context.Context) (*vault.Material, func(context.Context) error, error) {
		client, err := (vault.SessionManager{Directory: filepath.Join(root, "vault")}).Client(ctx)
		if err != nil {
			return nil, nil, err
		}
		master, ticket, err := client.UseWithTicket(ctx)
		if err != nil {
			return nil, nil, err
		}
		validate := func(ctx context.Context) error {
			current, next, err := client.UseWithTicket(ctx)
			if err != nil {
				return err
			}
			defer current.Destroy()
			if next.SessionID != ticket.SessionID || next.VaultID != ticket.VaultID || next.Epoch != ticket.Epoch {
				return vault.ErrInputChanged
			}
			return nil
		}
		return master, validate, nil
	}}
}

// CaptureCurrent selects the storage/session flow before preparing one actual
// execution. It never initializes protection or prompts from an agent command.
func CaptureCurrent(ctx context.Context, root, profile, env string, inject bool, now time.Time, selection ...string) (*Snapshot, *protocol.Error) {
	if !project.ValidProfile(profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	directory := filepath.Join(root, "vault")
	if _, err := (vault.KeyFileStore{Directory: directory}).Read(); err != nil {
		if errors.Is(err, vault.ErrKeyMissing) {
			return CapturePublic(ctx, root, profile, env, inject, now, selection...)
		}
		return nil, VaultError(ctx, err)
	}
	client, err := (vault.SessionManager{Directory: directory}).Client(ctx)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	return CaptureVaultSession(ctx, root, profile, env, inject, now, client, selection...)
}
func (s ModernStore) PlanImport(ctx context.Context, input map[string]string, variables []string, env string, overwrite bool) (result []ImportItem, possible bool, condition *protocol.Error) {
	defer func() { Recover(condition, s.root(), s.Profile, env, "", s.Invocation) }()
	if !project.ValidProfile(s.Profile) {
		return nil, false, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	master, id, err := s.scope(ctx)
	if err != nil {
		return nil, false, VaultError(ctx, err)
	}
	defer master.Destroy()
	release, err := maintenance.AcquireShared(ctx, s.root())
	if err != nil {
		return nil, false, VaultError(ctx, err)
	}
	p, key, _, err := s.readHeld(ctx, master, id)
	release()
	if err != nil {
		return nil, false, VaultError(ctx, err)
	}
	defer key.Destroy()
	var items []ImportItem
	var applicable bool
	var failure *protocol.Error
	_, err = p.Batch(ctx, key, func(m *vault.Mutation) error {
		editor := newEditor(ctx, p, m, key, master, id)
		items, applicable, failure = editor.PlanImport(input, variables, env, overwrite)
		if failure != nil {
			return failure
		}
		return nil
	})
	if err != nil {
		if failure != nil {
			return nil, false, failure
		}
		return nil, false, VaultError(ctx, err)
	}
	return items, applicable, nil
}

// CompletionNames reads only names from stable atomic profile images. It never
// creates locks, unlocks a key session or performs recovery during shell completion.
func (s ModernStore) CompletionNames(kind Kind, env string) ([]string, *protocol.Error) {
	raw, e := s.completionSnapshot()
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	state, e := metadataFromRaw(raw, s.Profile)
	if e != nil {
		return nil, e
	}
	if kind == "" {
		return state.EnvNames(), nil
	}
	items, e := state.List(kind, env)
	if e != nil {
		return nil, e
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Key)
	}
	return names, nil
}

// CompletionValueNames includes keys with only an environment slot: bundles
// select shared identities, not the effective keys of the common layer.
func (s ModernStore) CompletionValueNames() ([]string, *protocol.Error) {
	raw, e := s.completionSnapshot()
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	state, e := metadataFromRaw(raw, s.Profile)
	if e != nil {
		return nil, e
	}
	names := make([]string, 0, len(state.Keys))
	for name := range state.Keys {
		names = append(names, name)
	}
	return names, nil
}

// CompletionBundleNames returns public bundle names through the same passive
// snapshot boundary. It never consults a master/session or exposes value IDs.
func (s ModernStore) CompletionBundleNames() ([]string, *protocol.Error) {
	raw, e := s.completionSnapshot()
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	if raw == nil {
		return nil, nil
	}
	p, err := vault.DecodeProfile(raw)
	if err != nil {
		// Valid legacy metadata has no bundles. Do not migrate it on Tab.
		_, e := metadataFromRaw(raw, s.Profile)
		return nil, e
	}
	if p.Name != s.Profile {
		return nil, storageError()
	}
	names := make([]string, 0, len(p.Bundles))
	for _, bundle := range p.Bundles {
		names = append(names, bundle.Name)
	}
	return names, nil
}

func (s ModernStore) completionSnapshot() ([]byte, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return nil, storageError()
	}
	for attempt := 0; attempt < 2; attempt++ {
		first, err := profilekey.Snapshot(s.Directory, s.Profile, "values", vault.MaxDocumentBytes)
		if err != nil {
			continue
		}
		second, err := profilekey.Snapshot(s.Directory, s.Profile, "values", vault.MaxDocumentBytes)
		same := err == nil && bytes.Equal(first, second)
		clear(second)
		if same {
			return first, nil
		}
		clear(first)
	}
	return nil, storageError()
}

// RequireSecretWrite checks reusable key access before a CLI consumes secret
// input. Update still rechecks access, so a concurrent lock cannot authorize a
// later write. No storage gate is held while stdin or a file is read.
func (s ModernStore) RequireSecretWrite(ctx context.Context) (condition *protocol.Error) {
	defer func() { Recover(condition, s.root(), s.Profile, s.Invocation.Env, "", s.Invocation) }()
	if !project.ValidProfile(s.Profile) {
		return protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	master, _, err := s.scope(ctx)
	defer master.Destroy()
	if err != nil {
		return VaultError(ctx, err)
	}
	if master == nil {
		return VaultError(ctx, vault.ErrKeyMissing)
	}
	return nil
}
