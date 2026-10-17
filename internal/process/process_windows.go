package process

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
	"os"
)

func Execute(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	if runner, ok := ctx.Value(runnerKey{}).(Runner); ok {
		return runner(ctx, args, dir, env, in, out, diagnostic)
	}
	return ExecuteOwned(ctx, args, dir, env, in, out, diagnostic, nil)
}

// ExecuteOwned lets the Windows supervisor publish a successful start while
// keeping actual process/job/stream ownership inside this launcher.
func ExecuteOwned(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer, started func() *protocol.Error) (int, *protocol.Error) {
	if ctx.Err() != nil {
		return 130, nil
	}
	command, e := prepareInvocation(ctx, args, dir, env)
	if e != nil {
		return 0, e
	}
	command.Stdin, command.Stdout, command.Stderr = in, out, diagnostic
	child, err := startNativeOwned(ctx, command)
	if err != nil {
		if ctx.Err() != nil {
			return 130, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return 0, protocol.NewError("command_not_found", "Executable or execution directory was not found.", 127, nil)
		}
		return 0, protocol.NewError("execution_failed", "Cannot start the requested command with its Windows process ownership.", 126, nil)
	}
	defer child.cleanup()
	if started != nil {
		if e = started(); e != nil {
			_ = child.closeJob()
			_, _ = child.wait(context.Background())
			return 0, e
		}
	}
	code, err := child.wait(ctx)
	if err != nil {
		return 0, protocol.NewError("io_error", "Command process or stream handling failed.", 1, nil)
	}
	return code, nil
}
