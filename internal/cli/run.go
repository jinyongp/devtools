package cli

import (
	"context"
	"os"

	"github.com/jinyongp/devtools/internal/doctor"
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
	requirements := project.Requirements{}
	var command project.Command
	if len(request.Args) == 1 {
		p, err = project.Resolve(".", "")
		if err != nil {
			return nil, err
		}
		var exists bool
		command, exists = p.Commands[request.Args[0]]
		if !exists {
			return nil, protocol.NewError("command_not_found", "The selected project command is not defined.", 3, nil)
		}
		args = append(append([]string{}, command.Exec...), request.Child...)
		requirements = p.Requirements.Merge(command.Requirements)
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
	var state *values.State
	if inject || len(requirements.Vars)+len(requirements.Secs) > 0 || len(command.Bind) > 0 {
		directory, err := a.dataDirectory()
		if err != nil {
			return nil, err
		}
		state, err = (values.Store{Directory: directory, Profile: p.Profile}).Read()
		if err != nil {
			return nil, err
		}
		if inject {
			injected, err = state.Environment(env)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(command.Bind) > 0 || len(command.Serve) > 0 {
		s, e := a.portStore()
		if e != nil {
			return nil, e
		}
		defaults, e := portDefaults()
		if e != nil {
			return nil, e
		}
		prepared, release, e := s.Prepare(ctx, p, command, env, state, defaults, false)
		if e != nil {
			return nil, e
		}
		defer release()
		args = append(prepared.Args, request.Child...)
		for k, v := range prepared.Bind {
			injected[k] = v
		}
	}
	if !requirements.Empty() {
		checks := doctor.CheckRequirements(ctx, doctor.Input{Directory: dir, Env: env, Requirements: requirements, State: state, Inject: inject, Executable: args[0]})
		for _, check := range checks {
			if check.Status != "pass" {
				return nil, protocol.NewError("requirements_failed", "Command prerequisites are not satisfied. Run devtools doctor for diagnostics.", 3, map[string]any{"checks": checks})
			}
		}
	}
	exit, err := process.Execute(ctx, args, dir, process.Environment(os.Environ(), injected), streams.In, streams.Out, streams.Err)
	if err != nil {
		return nil, err
	}
	return processResult{ExitCode: exit}, nil
}
