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

// SessionMaster obtains the shared session once per storage operation. Native
// calls and reusable passphrase-session access occur before storage gates.
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
	return ModernStore{Directory: directory, Profile: profile, Master: SessionMaster(maintenance.Root(directory))}
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
func (s ModernStore) PlanImport(ctx context.Context, input map[string]string, variables []string, env string, overwrite bool) ([]ImportItem, bool, *protocol.Error) {
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
	defer release()
	p, key, _, err := s.readHeld(ctx, master, id)
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
	if !project.ValidProfile(s.Profile) {
		return nil, storageError()
	}
	for attempt := 0; attempt < 2; attempt++ {
		first, err := profilekey.Snapshot(s.Directory, s.Profile, "values", vault.MaxDocumentBytes)
		if err != nil {
			continue
		}
		state, e := metadataFromRaw(first, s.Profile)
		second, err := profilekey.Snapshot(s.Directory, s.Profile, "values", vault.MaxDocumentBytes)
		same := err == nil && bytes.Equal(first, second)
		clear(first)
		clear(second)
		if !same || e != nil {
			continue
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
	return nil, storageError()
}

// RequireSecretWrite checks reusable key access before a CLI consumes secret
// input. Update still rechecks access, so a concurrent lock cannot authorize a
// later write. No storage gate is held while stdin or a file is read.
func (s ModernStore) RequireSecretWrite(ctx context.Context) *protocol.Error {
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
