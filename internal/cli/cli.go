// Package cli owns command registration, validation, and execution.
package cli

import (
	"context"
	"io"
	"strings"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Option struct {
	Boolean     bool   `json:"boolean,omitempty"`
	Required    bool   `json:"required"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Pattern     string `json:"pattern,omitempty"`
	MinLength   int    `json:"minLength,omitempty"`
	MaxLength   int    `json:"maxLength,omitempty"`
}

type Command struct {
	Aliases      []string                                                  `json:"aliases"`
	Arguments    []Argument                                                `json:"arguments"`
	ChildArgs    bool                                                      `json:"accepts_child_args"`
	StreamOutput bool                                                      `json:"stream_output"`
	InputOneOf   []map[string]any                                          `json:"-"`
	Name         string                                                    `json:"name"`
	Description  string                                                    `json:"description"`
	Options      []Option                                                  `json:"options"`
	Output       map[string]any                                            `json:"output_schema"`
	Run          func(context.Context, IO, Request) (any, *protocol.Error) `json:"-"`
}

type App struct {
	commands      []Command
	dataDirectory func() (string, *protocol.Error)
}

type Argument struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Pattern  string `json:"pattern,omitempty"`
}
type Request struct {
	Options map[string]string
	Args    []string
	Child   []string
	Help    bool
}

func object(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringSchema() map[string]any { return map[string]any{"type": "string"} }

func New(version, commit string) *App {
	a := &App{dataDirectory: userDataDirectory}
	a.commands = []Command{
		{Name: "init", Description: "Create project configuration in the current directory without overwriting existing files.", Options: []Option{
			{Name: "profile", Description: "Project profile identifier.", Required: true, Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128},
		}, Output: object(map[string]any{"created": map[string]any{"type": "boolean"}, "config_path": stringSchema(), "profile": stringSchema()}, "created", "config_path", "profile"), Run: func(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
			return project.Init(".", request.Options["profile"])
		}},
		{Name: "help", Description: "Describe commands and their options.", Options: []Option{}, Output: map[string]any{"$ref": "#/$defs/catalog"}, Run: func(context.Context, IO, Request) (any, *protocol.Error) { return a.catalog(), nil }},
		{Name: "version", Description: "Report build and protocol versions.", Options: []Option{}, Output: object(map[string]any{"version": stringSchema(), "commit": stringSchema()}, "version", "commit"), Run: func(context.Context, IO, Request) (any, *protocol.Error) {
			return map[string]string{"version": version, "commit": commit}, nil
		}},
		{Name: "schema", Description: "Describe the machine interface using JSON Schema.", Options: []Option{}, Output: map[string]any{"$ref": "#/$defs/catalog"}, Run: func(context.Context, IO, Request) (any, *protocol.Error) { return a.catalog(), nil }},
		{Name: "project inspect", Description: "Resolve a project profile and user data paths without reading secrets.", Options: []Option{
			{Name: "dir", Description: "Directory to search from.", Default: ".", MinLength: 1},
			{Name: "profile", Description: "Explicit profile; bypasses configuration lookup.", Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128},
		}, Output: object(map[string]any{
			"project": object(map[string]any{"profile": stringSchema(), "source": map[string]any{"enum": []string{"flag", "file"}}, "config_path": stringSchema(), "root": stringSchema()}, "profile", "source"),
			"paths":   object(map[string]any{"config": stringSchema(), "data": stringSchema(), "cache": stringSchema()}, "config", "data", "cache"),
		}, "project", "paths"), Run: inspect},
	}
	a.registerTools()
	for i := range a.commands {
		if a.commands[i].Aliases == nil {
			a.commands[i].Aliases = []string{}
		}
		if a.commands[i].Arguments == nil {
			a.commands[i].Arguments = []Argument{}
		}
	}
	return a
}

func inspect(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	options := request.Options
	p, err := project.Resolve(options["dir"], options["profile"])
	if err != nil {
		return nil, err
	}
	dirs, pathErr := paths.Current()
	if pathErr != nil {
		return nil, protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
	}
	return struct {
		Project project.Context   `json:"project"`
		Paths   paths.Directories `json:"paths"`
	}{p, dirs}, nil
}

func (a *App) Run(ctx context.Context, args []string, streams IO) int {
	fail := func(err *protocol.Error) int {
		if protocol.Failure(streams.Err, err) != nil {
			return 1
		}
		return err.ExitCode
	}
	if ctx.Err() != nil {
		return fail(protocol.NewError("canceled", "Execution canceled.", 130, nil))
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		args = []string{"help"}
	}
	if len(args) == 1 && args[0] == "--version" {
		args = []string{"version"}
	}
	var selected *Command
	var rest []string
	for i := range a.commands {
		for _, name := range append([]string{a.commands[i].Name}, a.commands[i].Aliases...) {
			words := strings.Fields(name)
			if len(args) >= len(words) && strings.Join(args[:len(words)], " ") == name {
				selected, rest = &a.commands[i], args[len(words):]
				break
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		return fail(protocol.NewError("invalid_argument", "Unknown command. Run devtools schema to discover commands.", 2, nil))
	}
	request, parseErr := parseRequest(*selected, rest)
	if parseErr != nil {
		return fail(parseErr)
	}
	if request.Help {
		if protocol.Success(streams.Out, selected) != nil {
			return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
		}
		return 0
	}
	data, err := selected.Run(ctx, streams, request)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(protocol.NewError("canceled", "Execution canceled.", 130, nil))
	}
	if protocol.Success(streams.Out, data) != nil {
		return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
	}
	return 0
}
