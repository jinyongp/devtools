package netsetup

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
)

func OpenIdentity(path string) (*os.File, Identity, *protocol.Error) {
	return nil, Identity{}, protocol.NewError("unsupported_platform", "Windows network setup is not implemented in this intermediate build.", 3, nil)
}
