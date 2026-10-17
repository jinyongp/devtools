//go:build !windows

package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func removePrivateImage(_ context.Context, path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
