package ports

import (
	"context"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) publishRegistry(b []byte) *protocol.Error {
	r, err := platformfs.OpenRoot(s.Directory, false)
	if err != nil {
		return storageError()
	}
	defer r.Close()
	if err = r.WriteFile(context.Background(), "registry.json", b, true); err != nil {
		return storageError()
	}
	return nil
}
