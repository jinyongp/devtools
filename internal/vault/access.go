package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
)

// NativeProtection selects only the host's existing OS protection service. It
// never starts or installs a keyring, and never falls back to passphrase mode.
func NativeProtection(vaultID string, interactive bool) (Protector, error) {
	backend := NativeBackend()
	if backend == "" {
		return nil, ErrUnavailable
	}
	return nativeProtector(backend, vaultID, interactive)
}
func NativeBackend() string {
	switch runtime.GOOS {
	case "darwin":
		return "keychain"
	case "windows":
		return "dpapi"
	case "linux":
		return "secret-service"
	}
	return ""
}

// InspectSession is passive: it neither creates a daemon nor unlocks/touches an
// idle session. A missing key beside protected data is recovery, not empty state.
func InspectSession(ctx context.Context, directory string) (SessionStatus, error) {
	if err := ctx.Err(); err != nil {
		return SessionStatus{}, err
	}
	if _, err := (KeyFileStore{Directory: directory}).Read(); err != nil {
		status := SessionStatus{State: RecoveryRequired}
		if !errors.Is(err, ErrKeyMissing) {
			return status, nil
		}
		root := filepath.Dir(directory)
		if _, protected, e := maintenance.ProtectedRootHeld(root); e != nil || protected {
			return status, nil
		}
		if _, e := os.Lstat(filepath.Join(root, catalogRelative)); !errors.Is(e, os.ErrNotExist) {
			return status, nil
		}
		return SessionStatus{State: Uninitialized}, nil
	}
	if client, err := OpenSessionClient(directory); err == nil {
		if status, err := client.Status(ctx); err == nil {
			return status, nil
		}
	}
	return NewKeySession(directory).Status(), nil
}

// VerifyStorage authenticates the catalog, every sealed envelope/keyset and each
// secret layer after recovery. Public-only files remain explicitly unsigned.
func (s FileStore) VerifyStorage(ctx context.Context) error {
	release, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return err
	}
	defer release()
	catalog, err := s.readCatalog()
	if err != nil {
		return err
	}
	names, err := profilekey.Enumerate(filepath.Join(s.Root, "profiles"), "values")
	if err != nil {
		return ErrRecovery
	}
	selected := map[string]bool{}
	for _, entry := range catalog.Entries {
		selected[entry.Name] = true
	}
	for _, name := range names {
		selected[name] = true
	}
	for name := range selected {
		p, key, err := s.ReadAnyWithMasterHeld(ctx, name, s.CatalogKey)
		if err != nil {
			return err
		}
		for _, value := range p.Values {
			if value.Kind != Secret {
				continue
			}
			for _, slot := range value.Slots {
				plain, e := p.open(value, slot, key)
				clear(plain)
				if e != nil {
					key.Destroy()
					return e
				}
			}
		}
		key.Destroy()
	}
	return nil
}
