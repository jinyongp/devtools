package hosts

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s System) Apply(ctx context.Context, q Request) *protocol.Error {
	return protocol.NewError("unsupported_platform", "Windows hosts changes are not implemented in this intermediate build.", 3, nil)
}
