package cli

import (
	"context"
	"os"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func (a *App) runCommand(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	var p project.Context
	var err *protocol.Error
	args, dir, inject := request.Child, ".", true
	env := request.Options["env"]
	if len(request.Args) == 1 {
		p, err = project.Resolve(".", "")
		if err != nil {
			return nil, err
		}
		command, exists := p.Commands[request.Args[0]]
		if !exists {
			return nil, protocol.NewError("command_not_found", "The selected project command is not defined.", 3, nil)
		}
		args = append(append([]string{}, command.Exec...), request.Child...)
		dir, inject = p.Root, command.Inject
		if _, explicit := request.Options["env"]; explicit {
			inject = true
		} else {
			env = command.Env
		}
		if profile := request.Options["profile"]; profile != "" {
			p.Profile = profile
		}
	} else {
		if len(args) == 0 {
			return nil, argumentError("Supply a configured command name or an executable after --.", "command")
		}
		p, err = project.Resolve(".", request.Options["profile"])
		if err != nil {
			return nil, err
		}
	}
	injected := map[string]string{}
	if inject {
		directory, err := a.dataDirectory()
		if err != nil {
			return nil, err
		}
		state, err := (values.Store{Directory: directory, Profile: p.Profile}).Read()
		if err != nil {
			return nil, err
		}
		injected, err = state.Environment(env)
		if err != nil {
			return nil, err
		}
	}
	exit, err := process.Execute(ctx, args, dir, process.Environment(os.Environ(), injected), streams.In, streams.Out, streams.Err)
	if err != nil {
		return nil, err
	}
	return processResult{ExitCode: exit}, nil
}
