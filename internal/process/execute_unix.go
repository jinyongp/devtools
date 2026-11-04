//go:build !windows

package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"syscall"

	"github.com/jinyongp/devtools/internal/protocol"
)

func Execute(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	if runner, ok := ctx.Value(runnerKey{}).(Runner); ok {
		return runner(ctx, args, dir, env, in, out, diagnostic)
	}
	return ExecuteOwned(ctx, args, dir, env, in, out, diagnostic, nil)
}

// ExecuteOwned keeps the group leader unreaped until its owned descendants have
// finished. Supervisors and foreground clients use this same lifetime boundary.
func ExecuteOwned(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer, started func() *protocol.Error) (int, *protocol.Error) {
	if len(args) == 0 || args[0] == "" {
		return 0, protocol.NewError("invalid_argument", "Specify a command to execute.", 2, nil)
	}
	if ctx.Err() != nil {
		return 0, protocol.NewError("canceled", "Execution canceled.", 130, nil)
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return 0, protocol.NewError("io_error", "Cannot resolve execution directory.", 1, nil)
	}
	path := executablePath(ctx)
	if path == "" {
		var lookupErr *protocol.Error
		path, lookupErr = LookPath(args[0], absolute, env)
		if lookupErr != nil {
			return 0, lookupErr
		}
	}
	if !filepath.IsAbs(path) {
		return 0, protocol.NewError("invalid_argument", "Prepared executable path must be absolute.", 2, nil)
	}
	command := exec.Command(path, args[1:]...)
	command.Args[0] = args[0]
	command.Dir, command.Env = absolute, env
	command.Stdin, command.Stdout, command.Stderr = in, out, diagnostic
	tty, interactive := detectTTY(in, out, diagnostic)
	if interactive {
		command.SysProcAttr = tty.childAttrs()
	} else {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	command.WaitDelay = stopGrace
	if err := command.Start(); err != nil {
		if interactive {
			_ = tty.restoreAfterStartFailure()
		}
		if errors.Is(err, os.ErrNotExist) {
			return 0, protocol.NewError("command_not_found", "Executable or execution directory was not found.", 127, nil)
		}
		return 0, protocol.NewError("execution_failed", "Cannot start the requested command.", 126, nil)
	}
	startErr := NotifyStarted(ctx)
	if startErr == nil && started != nil {
		startErr = started()
	}
	if startErr != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		if interactive {
			_ = tty.restoreAfterStartFailure()
		}
		return 0, startErr
	}
	cleanupErr := waitOwnedGroup(ctx, command.Process.Pid, tty)
	err = command.Wait()
	if interactive {
		_ = tty.restoreIfOwned(command.Process.Pid)
	}
	if cleanupErr != nil {
		return 0, cleanupFailure("process_group")
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	return 0, protocol.NewError("io_error", "Command stream handling failed.", 1, nil)
}
