package expose

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/tasks"
)

func cloudflaredExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func startCloudflaredSupervisor(command *exec.Cmd) error { return process.StartDetached(command) }

// The private supervisor owns a separate console and the provider's kill-on-close
// Job. Parent stdin is only a lifeline, never the provider's stdin; EOF cancels
// the owned tree even after the launching daemon crashes.
func RunCloudflaredSupervisor(ctx context.Context, executable, origin string, in io.Reader, out, diagnostic io.Writer, statePaths ...string) int {
	if !ValidOrigin(origin) || !filepath.IsAbs(executable) || process.RequireDedicatedConsole() != nil {
		return 2
	}
	if len(statePaths) > 0 && statePaths[0] != "" {
		path := statePaths[0]
		if tasks.PrivateDir(filepath.Dir(path)) != nil || tasks.WritePrivate(path, map[string]string{"state": "running"}) != nil {
			return 1
		}
		defer func() { _ = tasks.WritePrivate(path, map[string]string{"state": "stopped"}) }()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, in); cancel() }()
	code, failure := process.ExecuteOwned(ctx, []string{executable, "tunnel", "--no-autoupdate", "--url", origin}, "", os.Environ(), nil, out, diagnostic, nil)
	if failure != nil {
		return 126
	}
	if ctx.Err() != nil {
		return 0
	}
	return code
}
