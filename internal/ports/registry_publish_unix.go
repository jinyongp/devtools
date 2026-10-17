//go:build !windows

package ports

import (
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) publishRegistry(b []byte) *protocol.Error {
	f, err := os.CreateTemp(s.Directory, ".update-*")
	if err != nil {
		return storageError()
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return storageError()
	}
	if os.Rename(f.Name(), filepath.Join(s.Directory, "registry.json")) != nil {
		return storageError()
	}
	d, err := os.Open(s.Directory)
	if err != nil {
		return storageError()
	}
	defer d.Close()
	if d.Sync() != nil {
		return storageError()
	}
	return nil
}
