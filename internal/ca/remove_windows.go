package ca

import (
	"context"
	"github.com/jinyongp/devtools/internal/platformfs"
)

func removeMaterial(ctx context.Context, path string) error {
	root, err := platformfs.OpenRoot(path, false)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveFile(ctx, "material.json")
}
