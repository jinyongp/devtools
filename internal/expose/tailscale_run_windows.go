package expose

import (
	"context"
	"github.com/jinyongp/devtools/internal/process"
	"io"
	"os"
)

func runTailscaleCommand(ctx context.Context, path string, args []string, out, diagnostic io.Writer) bool {
	code, failure := process.ExecuteOwned(ctx, append([]string{path}, args...), "", os.Environ(), nil, out, diagnostic, nil)
	return failure == nil && code == 0
}
