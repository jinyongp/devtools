// Package cli owns command registration, validation, and execution.
package cli

import (
	"context"
	"io"
	"strings"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/skills"
)

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Option struct {
	Repeatable  bool   `json:"repeatable,omitempty"`
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
	BodySchema   map[string]any                                            `json:"body_schema,omitempty"`
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
	Options     map[string]string
	ListOptions map[string][]string
	Args        []string
	Child       []string
	Help        bool
}
type processResult struct{ ExitCode int }

func object(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringSchema() map[string]any { return map[string]any{"type": "string"} }

func New(version, commit string) *App {
	a := &App{dataDirectory: userDataDirectory}
	a.commands = []Command{
		{Name: "skill", Description: "Print the bundled agent SKILL.md for installation.", StreamOutput: true, Output: map[string]any{"type": "string"}, Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			if _, err := io.WriteString(streams.Out, skills.Devtools); err != nil {
				return nil, protocol.NewError("io_error", "Cannot write skill.", 1, nil)
			}
			return processResult{ExitCode: 0}, nil
		}},
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
	a.registerImport()
	a.registerTasks()
	a.registerDashboard()
	a.registerDoctor()
	a.registerPorts()
	a.registerBackup()
	a.registerProcesses()
	a.registerCleanup()
	a.registerUpdate()
	a.registerCompletion()
	for i := range a.commands {
		if a.commands[i].Name == "schema" || a.commands[i].Name == "help" {
			a.commands[i].Arguments = []Argument{{Name: "command"}, {Name: "subcommand"}, {Name: "action"}, {Name: "operation"}}
			a.commands[i].Output = map[string]any{"type": "object"}
			if a.commands[i].Name == "schema" {
				a.commands[i].Description = "List command groups or return one command's JSON schema."
				a.commands[i].Options = []Option{{Name: "all", Boolean: true, Description: "Return the complete catalog (large output)."}}
			} else {
				a.commands[i].Description = "Show concise text usage for a command or group."
				a.commands[i].StreamOutput = true
			}
		}
	}
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
	if args[0] == "help" || args[0] == "schema" || ((args[len(args)-1] == "--help" || args[len(args)-1] == "-h") && !strings.Contains(strings.Join(args[:len(args)-1], " "), "--")) {
		schema := args[0] == "schema"
		targetArgs := args[1:]
		if len(targetArgs) == 1 && (targetArgs[0] == "--help" || targetArgs[0] == "-h") {
			schema = false
			targetArgs = []string{args[0]}
		}
		if args[0] != "help" && !schema {
			targetArgs = args[:len(args)-1]
		}
		if schema && len(targetArgs) == 1 && targetArgs[0] == "--all" {
			if protocol.Success(streams.Out, a.catalog()) != nil {
				return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
			}
			return 0
		}
		data, text, found := a.discovery(strings.Join(targetArgs, " "), schema)
		if !found {
			if !schema {
				if writeHelp(streams.Err, "Unknown command. Run devtools --help.\n") != nil {
					return 1
				}
				return 2
			}
			return fail(protocol.NewError("invalid_argument", "Unknown help or schema target. Run devtools --help.", 2, nil))
		}
		var err error
		if schema {
			err = protocol.Success(streams.Out, data)
		} else {
			err = writeHelp(streams.Out, text)
		}
		if err != nil {
			return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
		}
		return 0
	}
	var selected *Command
	var rest []string
	for i := range a.commands {
		for _, name := range append([]string{a.commands[i].Name}, a.commands[i].Aliases...) {
			words := strings.Fields(name)
			if len(args) >= len(words) && strings.Join(args[:len(words)], " ") == name && (selected == nil || len(words) > len(args)-len(rest)) {
				selected, rest = &a.commands[i], args[len(words):]
			}
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
		_, text, _ := a.discovery(selected.Name, false)
		if writeHelp(streams.Out, text) != nil {
			return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
		}
		return 0
	}
	data, err := selected.Run(ctx, streams, request)
	if err != nil {
		return fail(err)
	}
	if result, ok := data.(processResult); ok {
		return result.ExitCode
	}
	if ctx.Err() != nil {
		return fail(protocol.NewError("canceled", "Execution canceled.", 130, nil))
	}
	if protocol.Success(streams.Out, data) != nil {
		return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
	}
	return 0
}
