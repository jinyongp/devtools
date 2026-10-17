package expose

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
)

func RunCloudflaredSupervisor(ctx context.Context, executable, origin string, in io.Reader, out, diagnostic io.Writer, statePaths ...string) int {
	return 3
}
func (p Cloudflared) Start(ctx context.Context, r Record, origin string) (ProviderSession, *protocol.Error) {
	return nil, protocol.NewError("unsupported_platform", "Windows provider launch is not implemented in this intermediate build.", 3, nil)
}
