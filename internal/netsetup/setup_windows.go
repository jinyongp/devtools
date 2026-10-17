package netsetup

import (
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/winimage"
	"os"
)

func OpenIdentity(path string) (*os.File, Identity, *protocol.Error) {
	file, image, err := winimage.Open(path)
	if err != nil {
		return nil, Identity{}, failure(err.Code)
	}
	return file, Identity{Path: image.Path, Device: image.Device, Inode: image.Inode, Digest: image.Digest}, nil
}
func setupIdentityPath(path string) (string, error) { return location.Canonical(path) }
