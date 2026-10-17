package profiletransfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"filippo.io/age"
	"github.com/jinyongp/devtools/internal/platformfs"
)

func ensurePrivateDirectory(path string) error {
	r, err := platformfs.OpenRoot(path, true)
	if errors.Is(err, platformfs.ErrUnsafe) {
		return ErrInvalidIdentity
	}
	if err != nil {
		return err
	}
	return r.Close()
}
func stageIdentity(path string, identity *age.X25519Identity) error {
	r, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.WriteFile(context.Background(), filepath.Base(path), []byte(identity.String()+"\n"), false)
}
func publishPendingIdentity(pending, target string) (*age.X25519Identity, error) {
	identity, err := readIdentity(pending)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrIdentityMissing
	}
	if err != nil {
		return nil, ErrInvalidIdentity
	}
	r, err := platformfs.OpenRoot(filepath.Dir(pending), false)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if filepath.Dir(pending) != filepath.Dir(target) {
		return nil, ErrInvalidIdentity
	}
	if err = r.PublishFile(context.Background(), filepath.Base(pending), filepath.Base(target), false); err != nil {
		return nil, err
	}
	return identity, nil
}
func reconcilePendingIdentity(pending, target string) error {
	// MoveFileEx publishes the single pending image atomically. Coexisting names
	// are not a recoverable Windows publication state; preserve both on conflict.
	_, err := os.Lstat(pending)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrInvalidIdentity
}
