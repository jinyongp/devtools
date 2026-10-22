package services

import (
	"context"
	"github.com/jinyongp/devtools/internal/doctor"
	"os"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// PrepareFrozenInput is shared by CLI and dashboard initiators. All value,
// configuration and tool work finishes before admission or stopping an old app.
func PrepareFrozenInput(ctx context.Context, r Record, n *network.ExecutionSnapshot, dependenciesFor func(execution.Command) (execution.Dependencies, *protocol.Error)) (execution.Prepared, *protocol.Error) {
	p, e := project.Resolve(r.Directory, "")
	if e != nil {
		return execution.Prepared{}, e
	}
	if p.Profile != r.Profile || p.Root != r.Directory {
		return execution.Prepared{}, protocol.NewError("instance_conflict", "Project identity changed.", 3, nil)
	}
	command, e := execution.ResolveConfigured(p, r.Command, r.EnvOverride, nil)
	if e != nil {
		return execution.Prepared{}, e
	}
	if r.BundleOverride != nil {
		command.Selection = *r.BundleOverride
	}
	command.Network = n
	dependencies, e := dependenciesFor(command)
	if e != nil {
		return execution.Prepared{}, e
	}
	dependencies.CaptureOutput = r.Capture
	dependencies.DeferServing = true
	dependencies.IsolatedRoot = r.IsolatedRoot
	prepared, e := execution.PrepareInput(ctx, command, dependencies, os.Environ(), execution.PlanOptions{Mode: "managed"})
	if e != nil {
		return execution.Prepared{}, e
	}
	checks := prepared.Checks(ctx, os.Environ(), false)
	if !execution.ChecksPassed(checks) {
		prepared.Release()
		return execution.Prepared{}, doctor.Failure(checks, command.Name, command.Directory, command.Project.Profile, command.Env, command.Selection, "managed", command.Inject)
	}
	return prepared, nil
}
