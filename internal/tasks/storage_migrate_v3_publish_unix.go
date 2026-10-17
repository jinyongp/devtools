//go:build !windows

package tasks

import (
	"path/filepath"
)

func generationDurable(resolution v3Resolution) error {
	if err := syncPrivateDirectory(resolution.Root); err != nil {
		return err
	}
	generations := filepath.Dir(resolution.Root)
	if err := syncPrivateDirectory(generations); err != nil {
		return err
	}
	return syncPrivateDirectory(filepath.Dir(generations))
}
