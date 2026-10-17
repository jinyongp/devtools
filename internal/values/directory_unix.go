//go:build !windows

package values

import (
	"errors"

	"os"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !platformfs.DirectoryPrivate(path, info) {
		return errors.New("private directory required")
	}
	return nil
}
