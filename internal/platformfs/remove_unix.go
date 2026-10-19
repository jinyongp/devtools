//go:build linux || darwin

package platformfs

import (
	"context"
	"os"
	"path/filepath"
)

// RemoveFile validates the private leaf and keeps its parent chain pinned until
// deletion and directory synchronization complete. It never removes directories.
func (r *Root) RemoveFile(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := r.child(name)
	if err != nil {
		return err
	}
	parent, err := guardDirectory(filepath.Dir(path), r.path, false)
	if err != nil {
		return err
	}
	defer parent.close()
	file, err := r.OpenFile(name, os.O_RDONLY)
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
