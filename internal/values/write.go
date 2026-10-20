package values

import (
	"bytes"
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

type writeBase struct {
	original   []byte
	exists     bool
	protection [32]byte
}

func (s ModernStore) writeScope(ctx context.Context) (*vault.Material, string, func(context.Context) error, error) {
	var validate func(context.Context) error
	if s.WriteMaster != nil {
		s.Master = func(ctx context.Context) (*vault.Material, error) {
			key, check, err := s.WriteMaster(ctx)
			validate = check
			return key, err
		}
	}
	key, id, err := s.scope(ctx)
	return key, id, validate, err
}
func (s ModernStore) readForEdit(ctx context.Context, master *vault.Material, vaultID string) (*vault.Profile, *vault.Material, writeBase, error) {
	var base writeBase
	release, err := maintenance.AcquireShared(ctx, s.root())
	if err != nil {
		return nil, nil, base, err
	}
	p, key, exists, err := s.readHeld(ctx, master, vaultID)
	if err == nil {
		base.exists = exists
		base.protection, err = (vault.KeyFileStore{Directory: filepath.Join(s.root(), "vault")}).WriteIdentity()
	}
	release()
	if err != nil {
		key.Destroy()
		return nil, nil, base, err
	}
	// The owned source no longer needs a gate while serializing/computing.
	base.original, err = p.Encode()
	if err != nil {
		key.Destroy()
		return nil, nil, base, err
	}
	return p, key, base, nil
}

// lockCommit always follows launch -> key-slot -> maintenance ordering. IPC
// validation finishes before the maintenance gate; no native work occurs here.
func (s ModernStore) lockCommit(ctx context.Context, base writeBase, promoted bool, validate func(context.Context) error) (func(), error) {
	directory := filepath.Join(s.root(), "vault")
	var releaseLaunch func()
	var err error
	if promoted {
		releaseLaunch, err = vault.AcquireLaunchMutation(ctx, directory)
	} else {
		releaseLaunch, err = maintenance.LockShared(ctx, filepath.Join(directory, "launch.lock"))
	}
	if err != nil {
		return nil, err
	}
	releaseSlot, err := maintenance.LockShared(ctx, filepath.Join(directory, "key-slot-write.lock"))
	if err != nil {
		releaseLaunch()
		return nil, err
	}
	releaseAll := func() { releaseSlot(); releaseLaunch() }
	if validate != nil {
		if err = validate(ctx); err != nil {
			releaseAll()
			return nil, err
		}
	}
	releaseGate, err := maintenance.AcquireExclusive(ctx, s.root())
	if err != nil {
		releaseAll()
		return nil, err
	}
	release := func() { releaseGate(); releaseAll() }
	identity, err := (vault.KeyFileStore{Directory: directory}).WriteIdentity()
	if err != nil {
		release()
		return nil, err
	}
	if identity != base.protection {
		release()
		return nil, vault.ErrInputChanged
	}
	return release, nil
}
func (s ModernStore) checkBase(base writeBase, current *vault.Profile, exists bool) *protocol.Error {
	if base.exists != exists {
		return failure("revision_conflict", "Profile changed during preparation; retry with current state.")
	}
	if !exists {
		return nil
	}
	observed, err := current.Encode()
	if err != nil {
		return VaultError(context.Background(), err)
	}
	if !bytes.Equal(base.original, observed) {
		return failure("revision_conflict", "Profile changed during preparation; retry with current state.")
	}
	return nil
}
