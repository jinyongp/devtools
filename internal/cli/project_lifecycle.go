package cli

import (
	"context"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/lifecycle"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/values"
)

func readinessSchema() map[string]any {
	return object(map[string]any{
		"ready":      map[string]any{"type": "boolean"},
		"checked_at": stringSchema(),
		"reason":     stringSchema(),
		"exit_code":  map[string]any{"type": []string{"integer", "null"}},
	}, "ready", "checked_at", "reason", "exit_code")
}

func managedProcessSchema() map[string]any {
	return object(map[string]any{
		"ready_configured": map[string]any{"type": "boolean"},
		"readiness":        readinessSchema(),
		"id":               stringSchema(),
		"profile":          stringSchema(),
		"instance_id":      stringSchema(),
		"directory":        stringSchema(),
		"command":          stringSchema(),
		"env":              stringSchema(),
		"env_override":     map[string]any{"type": []string{"string", "null"}},
		"capture_logs":     map[string]any{"type": "boolean"},
		"created_at":       stringSchema(),
		"started_at":       map[string]any{"type": []string{"string", "null"}},
		"ended_at":         map[string]any{"type": []string{"string", "null"}},
		"exit_code":        map[string]any{"type": []string{"integer", "null"}},
		"reason":           stringSchema(),
		"state":            stringSchema(),
		"previous_id":      stringSchema(),
	}, "ready_configured", "id", "profile", "instance_id", "directory", "command", "env", "env_override", "capture_logs", "created_at", "started_at", "ended_at", "exit_code", "reason", "state")
}

func lifecycleItemSchema() map[string]any {
	condition := object(map[string]any{
		"code":    stringSchema(),
		"message": stringSchema(),
		"details": map[string]any{"type": "object"},
	}, "code", "message")
	return object(map[string]any{
		"command":   stringSchema(),
		"status":    map[string]any{"enum": []string{"ready", "running", "stopped", "failed", "pending", "unchanged"}},
		"changed":   map[string]any{"type": "boolean"},
		"item":      managedProcessSchema(),
		"condition": condition,
	}, "command", "status", "changed")
}

func lifecycleOutputSchema() map[string]any {
	return object(map[string]any{
		"action":    map[string]any{"enum": []string{lifecycle.ActionUp, lifecycle.ActionDown}},
		"profile":   stringSchema(),
		"directory": stringSchema(),
		"changed":   map[string]any{"type": "boolean"},
		"replayed":  map[string]any{"type": "boolean"},
		"items":     map[string]any{"type": "array", "items": lifecycleItemSchema()},
	}, "action", "profile", "directory", "changed", "replayed", "items")
}

func projectStatusSchema() map[string]any {
	return object(map[string]any{
		"profile":   stringSchema(),
		"directory": stringSchema(),
		"items":     map[string]any{"type": "array", "items": managedProcessSchema()},
	}, "profile", "directory", "items")
}

func (a *App) projectLifecycleManager() (lifecycle.Manager, *protocol.Error) {
	directory, err := a.dataDirectory()
	if err != nil {
		return lifecycle.Manager{}, err
	}
	data := filepath.Dir(directory)
	if a.lifecycleManager == nil {
		return lifecycle.Manager{}, protocol.NewError("internal_error", "Project lifecycle manager is unavailable.", 1, nil)
	}
	manager := a.lifecycleManager(data)
	manager.Preflight = a.preflightProjectCommand
	return manager, nil
}

func (a *App) preflightProjectCommand(ctx context.Context, p project.Context, name string, envOverride *string) *protocol.Error {
	command, ok := p.Commands[name]
	if !ok {
		return protocol.NewError("command_not_found", "The selected project command is not defined.", 3, map[string]any{"command": name})
	}
	env := command.Env
	inject := command.Inject
	if envOverride != nil {
		env = *envOverride
		inject = true
	}
	requirements := p.Requirements.Merge(command.Requirements)
	var state *values.State
	if inject || len(requirements.Vars)+len(requirements.Secs) > 0 || len(command.Bind) > 0 {
		directory, err := a.dataDirectory()
		if err != nil {
			return err
		}
		state, err = (values.Store{Directory: directory, Profile: p.Profile}).Read()
		if err != nil {
			return err
		}
		if envErr := state.CheckEnv(env); envErr != nil {
			return envErr
		}
	}
	executable := command.Exec[0]
	var boundPath *string
	if len(command.Bind) > 0 || len(command.Serve) > 0 {
		store, err := a.portStore()
		if err != nil {
			return err
		}
		defaults, err := portDefaults()
		if err != nil {
			return err
		}
		prepared, _, prepareErr := store.Prepare(ctx, p, command, env, state, defaults, true)
		if prepareErr != nil {
			return prepareErr
		}
		executable = prepared.Args[0]
		if path, ok := prepared.Bind["PATH"]; ok {
			boundPath = &path
		}
	}
	checks := doctor.CheckRequirements(ctx, doctor.Input{Profile: p.Profile, Directory: p.Root, Env: env, Requirements: requirements, State: state, Inject: inject, Executable: executable, PathOverride: boundPath})
	for _, check := range checks {
		if check.Status != "pass" {
			return protocol.NewError("requirements_failed", "Project command prerequisites are not satisfied.", 3, map[string]any{"command": name, "checks": checks})
		}
	}
	return nil
}

func (a *App) registerProjectLifecycle() {
	commandArgument := Argument{Name: "command", Required: true, Repeatable: true, Pattern: project.ProfilePattern}
	optionalCommands := Argument{Name: "command", Repeatable: true, Pattern: project.ProfilePattern}
	directory := Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory."}

	a.commands = append(a.commands,
		Command{
			Name:        "project up",
			UniqueArgs:  true,
			Description: "Start one or more configured commands as a retry-safe project operation.",
			Arguments:   []Argument{commandArgument},
			Options: []Option{
				directory,
				{Name: "env", Pattern: project.ProfilePattern, MinLength: 1, Description: "Inject this environment for every selected command."},
				{Name: "capture-logs", Boolean: true, Description: "Retain bounded raw output for every selected command."},
				{Name: "timeout", Default: "30s", Description: "Readiness wait per selected command, greater than zero and at most 10m."},
				{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe project start."},
			},
			Output: lifecycleOutputSchema(),
			Run:    a.projectUp,
		},
		Command{
			Name:        "project status",
			UniqueArgs:  true,
			Description: "Show active managed executions for the current project instance.",
			Arguments:   []Argument{optionalCommands},
			Options:     []Option{directory},
			Output:      projectStatusSchema(),
			Run:         a.projectStatus,
		},
		Command{
			Name:        "project down",
			UniqueArgs:  true,
			Description: "Stop selected or all active managed executions for the current project instance.",
			Arguments:   []Argument{optionalCommands},
			Options: []Option{
				directory,
				{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe project stop."},
			},
			Output: lifecycleOutputSchema(),
			Run:    a.projectDown,
		},
	)
}

func resolveLifecycleProject(directory string) (project.Context, *protocol.Error) {
	p, err := project.Resolve(directory, "")
	if err != nil {
		return project.Context{}, err
	}
	if p.Source != "file" || p.Root == "" {
		return project.Context{}, protocol.NewError("project_not_found", "Project lifecycle requires devtools.toml.", 3, nil)
	}
	return p, nil
}

func (a *App) projectUp(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	p, err := resolveLifecycleProject(request.Options["dir"])
	if err != nil {
		return nil, err
	}
	timeout, parseErr := time.ParseDuration(request.Options["timeout"])
	if parseErr != nil || timeout <= 0 || timeout > 10*time.Minute {
		return nil, argumentError("Expected a positive duration up to 10m.", "timeout")
	}
	manager, err := a.projectLifecycleManager()
	if err != nil {
		return nil, err
	}
	input := lifecycle.Request{Action: lifecycle.ActionUp, Project: p, Commands: append([]string{}, request.Args...), Timeout: timeout, RequestID: request.Options["request-id"]}
	if env, ok := request.Options["env"]; ok {
		input.Env = &env
	}
	if capture, ok := request.Options["capture-logs"]; ok {
		value := capture == "true"
		input.Capture = &value
	}
	return manager.Apply(ctx, input)
}

func (a *App) projectDown(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	p, err := resolveLifecycleProject(request.Options["dir"])
	if err != nil {
		return nil, err
	}
	manager, err := a.projectLifecycleManager()
	if err != nil {
		return nil, err
	}
	return manager.Apply(ctx, lifecycle.Request{Action: lifecycle.ActionDown, Project: p, Commands: append([]string{}, request.Args...), RequestID: request.Options["request-id"]})
}

func (a *App) projectStatus(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	p, err := resolveLifecycleProject(request.Options["dir"])
	if err != nil {
		return nil, err
	}
	manager, err := a.projectLifecycleManager()
	if err != nil {
		return nil, err
	}
	items, statusErr := manager.Status(ctx, p, request.Args)
	if statusErr != nil {
		return nil, statusErr
	}
	return struct {
		Profile   string            `json:"profile"`
		Directory string            `json:"directory"`
		Items     []services.Record `json:"items"`
	}{Profile: p.Profile, Directory: p.Root, Items: items}, nil
}
