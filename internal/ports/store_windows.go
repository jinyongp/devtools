package ports

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
)

func (s Store) lock(ctx context.Context, name string, wait, create bool) (*os.File, *protocol.Error) {
	return nil, protocol.NewError("unsupported_platform", "Windows registry locking is not implemented in this intermediate build.", 3, nil)
}
