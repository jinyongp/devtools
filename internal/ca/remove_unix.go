//go:build !windows

package ca

import (
	"context"
	"os"
	"path/filepath"
)

func removeMaterial(_ context.Context, root string) error {
	if err := os.Remove(filepath.Join(root, "material.json")); err != nil {
		return err
	}
	directory, err := os.Open(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
