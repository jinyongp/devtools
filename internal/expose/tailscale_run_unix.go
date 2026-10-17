//go:build !windows

package expose

import (
	"context"
	"io"
	"os/exec"
	"time"
)

func runTailscaleCommand(ctx context.Context, path string, args []string, out, diagnostic io.Writer) bool {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout, cmd.Stderr = out, diagnostic
	cmd.WaitDelay = time.Second
	return cmd.Run() == nil
}
