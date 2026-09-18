package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func (a *App) executionDependencies(command execution.Command) (execution.Dependencies, *protocol.Error) {
	needsPorts := len(command.Bind)+len(command.Serve) > 0
	if !command.NeedsValues() && !needsPorts {
		return execution.Dependencies{}, nil
	}
	directory, err := a.dataDirectory()
	if err != nil {
		return execution.Dependencies{}, err
	}
	configRoot := ""
	if needsPorts {
		dirs, pathErr := paths.Current()
		if pathErr != nil {
			return execution.Dependencies{}, protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
		}
		configRoot = dirs.Config
	}
	return execution.DependenciesFor(command, filepath.Dir(directory), configRoot)
}

func (a *App) prepareExecution(ctx context.Context, command execution.Command, preview bool) (execution.Prepared, *protocol.Error) {
	dependencies, err := a.executionDependencies(command)
	if err != nil {
		return execution.Prepared{}, err
	}
	return execution.Prepare(ctx, command, dependencies, preview)
}

func (a *App) previewExecution(ctx context.Context, command execution.Command, state *values.State) (execution.Prepared, *protocol.Error) {
	dependencies, err := a.executionDependencies(command)
	if err != nil {
		return execution.Prepared{}, err
	}
	return execution.Preview(ctx, command, state, dependencies)
}

func (a *App) preflightProjectCommand(ctx context.Context, p project.Context, name string, envOverride *string) *protocol.Error {
	current, err := project.Resolve(p.Root, "")
	if err != nil {
		return err
	}
	if current.Root != p.Root || current.Profile != p.Profile {
		return protocol.NewError("instance_conflict", "Project identity changed.", 3, nil)
	}
	command, err := execution.ResolveConfigured(current, name, envOverride, nil)
	if err != nil {
		return err
	}
	dependencies, err := a.executionDependencies(command)
	if err != nil {
		return err
	}
	return execution.Preflight(ctx, command, dependencies, os.Environ())
}
