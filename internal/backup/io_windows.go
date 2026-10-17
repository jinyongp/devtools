package backup

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func ensureConfigDirectory(path string) error {
	r, e := platformfs.OpenRoot(path, true)
	if e != nil {
		return e
	}
	return r.Close()
}
func ensurePrivateDirectoryChain(root, directory string) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(directory) {
		return errors.New("absolute private directory required")
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return errors.New("private directory must be below data root")
	}
	r, err := platformfs.OpenRoot(root, true)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.MkdirAll(relative)
}
func exclusiveContext(ctx context.Context, path string, body []byte) error {
	return platformfs.WriteExternalPrivate(ctx, path, body, false)
}
