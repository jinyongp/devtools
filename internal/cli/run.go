package cli

import (
	"context"
	"os"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) runCommand(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	var command execution.Command
	if len(request.Args) == 1 {
		p, err := project.Resolve(".", "")
		if err != nil {
			return nil, err
		}
		if profile := request.Options["profile"]; profile != "" {
			p.Profile = profile
		}
		var envOverride *string
		if env, explicit := request.Options["env"]; explicit {
			envOverride = &env
		}
		command, err = execution.ResolveConfigured(p, request.Args[0], envOverride, request.Child)
		if err != nil {
			return nil, err
		}
	} else {
		if len(request.Child) == 0 {
			return nil, argumentError("Supply a configured command name or an executable after --.", "command")
		}
		p, err := project.Resolve(".", request.Options["profile"])
		if err != nil {
			return nil, err
		}
		command, err = execution.ResolveDirect(p, request.Child, ".", request.Options["env"])
		if err != nil {
			return nil, err
		}
	}
	prepared, err := a.prepareExecution(ctx, command, false)
	if err != nil {
		return nil, err
	}
	defer prepared.Release()
	checks := prepared.Checks(ctx, os.Environ(), false)
	if !execution.ChecksPassed(checks) {
		return nil, protocol.NewError("requirements_failed", "Command prerequisites are not satisfied. Run devtools doctor for diagnostics.", 3, map[string]any{"checks": checks})
	}
	exit, err := prepared.Execute(ctx, os.Environ(), nil, streams.In, streams.Out, streams.Err)
	if err != nil {
		return nil, err
	}
	return processResult{ExitCode: exit}, nil
}
