//go:build !windows

package expose

import (
	"context"
	"io"

	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

// RunCloudflaredSupervisor pairs the provider lifetime with a private stdin
// pipe held by its daemon. EOF also terminates descendants after daemon crash.
func RunCloudflaredSupervisor(ctx context.Context, executable, origin string, in io.Reader, out, diagnostic io.Writer, statePaths ...string) int {
	if len(statePaths) > 0 && statePaths[0] != "" {
		path := statePaths[0]
		if tasks.PrivateDir(filepath.Dir(path)) != nil || tasks.WritePrivate(path, map[string]string{"state": "running"}) != nil {
			return 1
		}
		defer func() { _ = tasks.WritePrivate(path, map[string]string{"state": "stopped"}) }()
	}
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return 2
	}
	if !strings.HasPrefix(executable, "/") {
		return 2
	}
	cmd := exec.Command(executable, "tunnel", "--no-autoupdate", "--url", origin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	cmd.WaitDelay = time.Second
	if cmd.Start() != nil {
		return 126
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	lost := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, in); close(lost) }()
	select {
	case e := <-done:
		if e == nil {
			return 0
		}
		return 1
	case <-ctx.Done():
	case <-lost:
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
	return 0
}

func cloudflaredExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}
func startCloudflaredSupervisor(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command.Start()
}
