package services

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func removePrivateImage(ctx context.Context, path string) error {
	r, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RemoveFile(ctx, filepath.Base(path))
}
