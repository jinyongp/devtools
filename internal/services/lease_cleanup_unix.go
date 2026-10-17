//go:build !windows

package services

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) cleanupLaunchStaging() *protocol.Error {
	root := s.root()
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storageError()
	}
	removed := false
	for _, entry := range entries {
		if !validLaunchStagingName(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return storageError()
		}
		if err := os.RemoveAll(path); err != nil {
			return storageError()
		}
		removed = true
	}
	if removed && syncDirectory(root) != nil {
		return storageError()
	}
	return nil
}
