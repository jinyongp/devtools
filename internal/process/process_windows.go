package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func Execute(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	if runner, ok := ctx.Value(runnerKey{}).(Runner); ok {
		return runner(ctx, args, dir, env, in, out, diagnostic)
	}
	if ctx.Err() != nil {
		return 130, nil
	}
	command, e := prepareInvocation(ctx, args, dir, env)
	if e != nil {
		return 0, e
	}
	command.Stdin, command.Stdout, command.Stderr = in, out, diagnostic
	command.WaitDelay = 3 * time.Second
	if err := command.Start(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, protocol.NewError("command_not_found", "Executable or execution directory was not found.", 127, nil)
		}
		return 0, protocol.NewError("execution_failed", "Cannot start the requested command.", 126, nil)
	}
	err := command.Wait()
	if ctx.Err() != nil {
		return 130, nil
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, protocol.NewError("io_error", "Command stream handling failed.", 1, nil)
}

// Detached managed processes remain unavailable until the W4 native launcher.
func StartDetached(command *exec.Cmd) error { return errors.ErrUnsupported }
