package cli

import (
	"context"
	"os"
	"sync"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (a *App) runCommand(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	var command execution.Command
	if len(request.Args) == 1 {
		p, err := a.resolveStoredProject(".", "")
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
		p, err := a.resolveStoredProject(".", request.Options["profile"])
		if err != nil {
			return nil, err
		}
		command, err = execution.ResolveDirect(p, request.Child, ".", request.Options["env"])
		if err != nil {
			return nil, err
		}
	}
	if selection, explicit := request.Options["bundle"]; explicit {
		command.Selection = selection
		command.BundleOverride = &selection
	}
	command.Invocation = request.Invocation
	if mode := request.Options["output"]; mode != "" {
		command.Output.Mode = redact.Mode(mode)
	}
	if mode := request.Options["stdout"]; mode != "" {
		command.Output.Stdout = redact.Mode(mode)
	}
	if mode := request.Options["stderr"]; mode != "" {
		command.Output.Stderr = redact.Mode(mode)
	}
	mode, modeErr := preparationMode(request.Options)
	if modeErr != nil {
		return nil, modeErr
	}
	if command.Name == "" && mode == network.PrepareOn {
		return nil, argumentError("--proxy requires a configured command with declared serve routes.", "proxy")
	}
	needed := false
	if command.Name != "" {
		var e *protocol.Error
		needed, e = a.needsProxyPreparation(command.Project, []string{command.Name}, mode)
		if e != nil {
			return nil, e
		}
	}
	if needed {
		var override *string
		if env, explicit := request.Options["env"]; explicit {
			override = &env
		}
		snapshots, e := a.prepareNetwork(ctx, command.Project, []string{command.Name}, nil, map[string]*string{command.Name: override}, streams, tasks.ID(), mode, false, map[string]string{command.Name: command.Selection})
		if e != nil {
			return nil, e
		}
		command.Network = snapshots[command.Name]
	}
	if command.Name != "" && !process.HasRunner(ctx) {
		command.ExecutionID = tasks.ID()
	}
	prepared, err := a.prepareExecution(ctx, command, false, streams)
	if err != nil {
		return nil, err
	}
	prepared.Release = sync.OnceFunc(prepared.Release)
	defer prepared.Release()
	checks := prepared.Checks(ctx, os.Environ(), false)
	if !execution.ChecksPassed(checks) {
		return nil, doctor.FailureFor(checks, command.Diagnostic("foreground"))
	}
	exit := 0
	if command.ExecutionID != "" {
		store, storeErr := a.serviceStore()
		if storeErr != nil {
			return nil, storeErr
		}
		exit, err = store.RunForeground(ctx, prepared, os.Environ(), streams.In, streams.Out, streams.Err)
		if err != nil && len(command.Serve) == 0 && prepared.Command.ExecutionID != "" && foregroundTrackingDenied(err) {
			// A one-off command must retain its existing read-only execution
			// contract. Service ownership, in contrast, requires registration.
			exit, err = prepared.Execute(ctx, os.Environ(), nil, streams.In, streams.Out, streams.Err)
		}
	} else {
		exit, err = prepared.Execute(ctx, os.Environ(), nil, streams.In, streams.Out, streams.Err)
	}
	if err != nil {
		return nil, err
	}
	return processResult{ExitCode: exit}, nil
}

func foregroundTrackingDenied(condition *protocol.Error) bool {
	if condition == nil || condition.Code != "io_error" {
		return false
	}
	return condition.Details["phase"] == "register_execution" && (condition.Details["reason"] == "access_denied" || condition.Details["reason"] == "read_only")
}
