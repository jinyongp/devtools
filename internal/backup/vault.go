package backup

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (e Engine) withVault(ctx context.Context) (Engine, func(), *protocol.Error) {
	if e.VaultMaster == nil || e.vaultMaster != nil {
		return e, func() {}, nil
	}
	master, err := e.VaultMaster(ctx)
	if err != nil {
		return e, nil, values.VaultError(ctx, err)
	}
	raw := master.CopyBytes()
	valid := len(raw) == 32
	clear(raw)
	if !valid {
		master.Destroy()
		return e, nil, values.VaultError(ctx, vault.ErrLocked)
	}
	k, err := (vault.KeyFileStore{Directory: filepath.Join(e.Data, "vault")}).Read()
	if err != nil {
		master.Destroy()
		return e, nil, values.VaultError(ctx, err)
	}
	e.vaultMaster = master
	e.vaultID = k.VaultID
	return e, master.Destroy, nil
}
func (e Engine) vaultStore() vault.FileStore {
	return vault.FileStore{Root: e.Data, VaultID: e.vaultID, CatalogKey: e.vaultMaster}
}
func (e Engine) valueSnapshotHeld(ctx context.Context, name string) ([]byte, error) {
	if e.vaultMaster != nil {
		return e.vaultStore().LogicalSnapshotHeld(ctx, name)
	}
	return profilekey.Snapshot(filepath.Join(e.Data, "profiles"), name, "values", limit)
}
func receiptScope(id string) string { return strings.ToLower(strings.ReplaceAll(id, "-", "")) }
func (e Engine) decodeReceipt(ctx context.Context, id string, body []byte) ([]byte, error) {
	if e.vaultMaster == nil {
		return body, nil
	}
	plain, err := vault.OpenProtected(ctx, e.vaultMaster, e.vaultID, receiptScope(id), vault.MaterialBackupReceipt, body)
	if err != nil {
		return nil, err
	}
	defer plain.Destroy()
	return plain.CopyBytes(), nil
}
func (e Engine) encodeReceipt(ctx context.Context, id string, body []byte) ([]byte, error) {
	if e.vaultMaster == nil {
		return body, nil
	}
	plain := vault.NewMaterial(body)
	defer plain.Destroy()
	return vault.SealProtected(ctx, e.vaultMaster, e.vaultID, receiptScope(id), vault.MaterialBackupReceipt, plain)
}
func (e Engine) restoreValueFilesHeld(ctx context.Context, name string, data []byte) (map[string][]byte, error) {
	if e.vaultMaster != nil {
		return values.PrepareVaultRestoreHeld(ctx, e.Data, e.vaultID, e.vaultMaster, name, data)
	}
	return profilekey.Replacement(filepath.Join(e.Data, "profiles"), name, "values", data)
}
func (e Engine) checkVaultRootHeld() error {
	id, protected, err := maintenance.ProtectedRootHeld(e.Data)
	if err != nil {
		return err
	}
	if protected && e.vaultMaster == nil {
		return errors.New("unsupported_storage")
	}
	if e.vaultMaster != nil && (!protected || id != e.vaultID) {
		return vault.ErrIntegrity
	}
	return nil
}

func destroySnapshot(s *Snapshot) {
	if s == nil {
		return
	}
	for i := range s.Profiles {
		clear(s.Profiles[i].Values)
		clear(s.Profiles[i].Tasks)
		s.Profiles[i].Values = nil
		s.Profiles[i].Tasks = nil
	}
}
