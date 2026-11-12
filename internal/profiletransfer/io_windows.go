package profiletransfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"

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
func stageIdentity(path string, body []byte) error {
	r, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.WriteFile(context.Background(), filepath.Base(path), body, false)
}
func publishIdentity(pending, target string) error {
	r, err := platformfs.OpenRoot(filepath.Dir(pending), false)
	if err != nil {
		return err
	}
	defer r.Close()
	if filepath.Dir(pending) != filepath.Dir(target) {
		return ErrInvalidIdentity
	}
	return r.PublishFile(context.Background(), filepath.Base(pending), filepath.Base(target), false)
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
