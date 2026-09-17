package cli

import (
	"context"
	"sort"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type projectCommandSummary struct {
	Name   string   `json:"name"`
	Exec   []string `json:"exec"`
	Inject bool     `json:"inject"`
	Env    string   `json:"env"`
	Serve  []string `json:"serve"`
}

type projectCommandBinding struct {
	Port     string  `json:"port,omitempty"`
	Profile  string  `json:"profile,omitempty"`
	Instance string  `json:"instance,omitempty"`
	Template *string `json:"template,omitempty"`
}

type projectCommandReady struct {
	Exec    []string `json:"exec"`
	Timeout string   `json:"timeout"`
}

type projectCommandTool struct {
	Executable  string   `json:"executable"`
	Version     string   `json:"version"`
	VersionArgs []string `json:"version_args"`
}

type projectCommandRequirements struct {
	Tools map[string]projectCommandTool `json:"tools"`
	Vars  []string                      `json:"vars"`
	Secs  []string                      `json:"secs"`
}

type projectCommandDetails struct {
	Name         string                           `json:"name"`
	Exec         []string                         `json:"exec"`
	Inject       bool                             `json:"inject"`
	Env          string                           `json:"env"`
	Serve        []string                         `json:"serve"`
	Bind         map[string]projectCommandBinding `json:"bind"`
	Requirements projectCommandRequirements       `json:"requirements"`
	Ready        *projectCommandReady             `json:"ready"`
}

func projectCommandSummarySchema() map[string]any {
	return object(map[string]any{
		"name":   stringSchema(),
		"exec":   map[string]any{"type": "array", "items": stringSchema()},
		"inject": map[string]any{"type": "boolean"},
		"env":    stringSchema(),
		"serve":  map[string]any{"type": "array", "items": stringSchema()},
	}, "name", "exec", "inject", "env", "serve")
}

func projectCommandRequirementsSchema() map[string]any {
	tool := object(map[string]any{
		"executable":   stringSchema(),
		"version":      stringSchema(),
		"version_args": map[string]any{"type": "array", "items": stringSchema()},
	}, "executable", "version", "version_args")
	return object(map[string]any{
		"tools": map[string]any{"type": "object", "additionalProperties": tool},
		"vars":  map[string]any{"type": "array", "items": stringSchema()},
		"secs":  map[string]any{"type": "array", "items": stringSchema()},
	}, "tools", "vars", "secs")
}

func projectCommandDetailsSchema() map[string]any {
	binding := object(map[string]any{
		"port":     stringSchema(),
		"profile":  stringSchema(),
		"instance": stringSchema(),
		"template": stringSchema(),
	})
	ready := object(map[string]any{
		"exec":    map[string]any{"type": "array", "items": stringSchema()},
		"timeout": stringSchema(),
	}, "exec", "timeout")
	ready["type"] = []string{"object", "null"}
	fields := projectCommandSummarySchema()["properties"].(map[string]any)
	fields["bind"] = map[string]any{"type": "object", "additionalProperties": binding}
	fields["requirements"] = projectCommandRequirementsSchema()
	fields["ready"] = ready
	return object(fields, "name", "exec", "inject", "env", "serve", "bind", "requirements", "ready")
}

func (a *App) registerCommands() {
	projectOptions := []Option{{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory."}}
	a.commands = append(a.commands,
		Command{
			Name:        "command list",
			Description: "List configured project commands and their execution summary.",
			Options:     projectOptions,
			Output: object(map[string]any{
				"profile": stringSchema(),
				"items":   map[string]any{"type": "array", "items": projectCommandSummarySchema()},
			}, "profile", "items"),
			Run: a.listProjectCommands,
		},
		Command{
			Name:        "command inspect",
			Description: "Show the effective definition of one configured project command.",
			Options:     projectOptions,
			Arguments:   []Argument{{Name: "command", Required: true, Pattern: project.ProfilePattern}},
			Output: object(map[string]any{
				"profile": stringSchema(),
				"item":    projectCommandDetailsSchema(),
			}, "profile", "item"),
			Run: a.inspectProjectCommand,
		},
		Command{
			Name:        "command run",
			Aliases:     []string{"run"},
			Description: "Execute a configured command or a command after -- with profile values.",
			Options:     profileOptions(true),
			Arguments:   []Argument{{Name: "command", Pattern: project.ProfilePattern}},
			ChildArgs:   true,
			OutputMode:  OutputPassthrough,
			Output:      map[string]any{},
			Run:         a.runCommand,
		},
	)
}

func (a *App) listProjectCommands(_ context.Context, _ IO, request Request) (any, *protocol.Error) {
	p, err := project.Resolve(request.Options["dir"], "")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(p.Commands))
	for name := range p.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	commands := make([]projectCommandSummary, 0, len(names))
	for _, name := range names {
		commands = append(commands, summarizeProjectCommand(name, p.Commands[name]))
	}
	return map[string]any{"profile": p.Profile, "items": commands}, nil
}

func (a *App) inspectProjectCommand(_ context.Context, _ IO, request Request) (any, *protocol.Error) {
	p, err := project.Resolve(request.Options["dir"], "")
	if err != nil {
		return nil, err
	}
	name := request.Args[0]
	command, exists := p.Commands[name]
	if !exists {
		return nil, protocol.NewError("command_not_found", "The selected project command is not defined.", 3, nil)
	}
	return map[string]any{"profile": p.Profile, "item": describeProjectCommand(name, command, p.Requirements)}, nil
}

func summarizeProjectCommand(name string, command project.Command) projectCommandSummary {
	return projectCommandSummary{
		Name:   name,
		Exec:   append([]string{}, command.Exec...),
		Inject: command.Inject,
		Env:    command.Env,
		Serve:  append([]string{}, command.Serve...),
	}
}

func describeProjectCommand(name string, command project.Command, requirements project.Requirements) projectCommandDetails {
	bind := make(map[string]projectCommandBinding, len(command.Bind))
	for key, binding := range command.Bind {
		bind[key] = projectCommandBinding{
			Port:     binding.Port,
			Profile:  binding.Profile,
			Instance: binding.Instance,
			Template: binding.Template,
		}
	}
	var ready *projectCommandReady
	if command.Ready != nil {
		ready = &projectCommandReady{Exec: append([]string{}, command.Ready.Exec...), Timeout: command.Ready.Duration().String()}
	}
	return projectCommandDetails{
		Name:         name,
		Exec:         append([]string{}, command.Exec...),
		Inject:       command.Inject,
		Env:          command.Env,
		Serve:        append([]string{}, command.Serve...),
		Bind:         bind,
		Requirements: describeProjectCommandRequirements(requirements.Merge(command.Requirements)),
		Ready:        ready,
	}
}

func describeProjectCommandRequirements(requirements project.Requirements) projectCommandRequirements {
	tools := make(map[string]projectCommandTool, len(requirements.Tools))
	for name, tool := range requirements.Tools {
		tool = tool.WithDefaults(name)
		tools[name] = projectCommandTool{
			Executable:  tool.Executable,
			Version:     tool.Version,
			VersionArgs: append([]string{}, tool.VersionArgs...),
		}
	}
	return projectCommandRequirements{
		Tools: tools,
		Vars:  append([]string{}, requirements.Vars...),
		Secs:  append([]string{}, requirements.Secs...),
	}
}
