// Package cli owns command registration, validation, and execution.
package cli

import (
	"context"
	"io"
	"strings"

	"github.com/jinyongp/devtools/internal/lifecycle"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/services"
)

const uuidPattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`
const positiveIntegerPattern = `^[1-9][0-9]*$`

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
	BodySchema  map[string]any                                            `json:"body_schema,omitempty"`
	Aliases     []string                                                  `json:"aliases"`
	Arguments   []Argument                                                `json:"arguments"`
	ChildArgs   bool                                                      `json:"accepts_child_args"`
	UniqueArgs  bool                                                      `json:"-"`
	OutputMode  OutputMode                                                `json:"output_mode"`
	InputOneOf  []map[string]any                                          `json:"-"`
	Name        string                                                    `json:"name"`
	Description string                                                    `json:"description"`
	Options     []Option                                                  `json:"options"`
	Output      map[string]any                                            `json:"output_schema"`
	Run         func(context.Context, IO, Request) (any, *protocol.Error) `json:"-"`
}

type App struct {
	commands         []Command
	dataDirectory    func() (string, *protocol.Error)
	proxyManager     func(string) proxy.Manager
	lifecycleManager func(string) lifecycle.Manager
}

type Argument struct {
	Name       string `json:"name"`
	Required   bool   `json:"required"`
	Repeatable bool   `json:"repeatable,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
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
	a := &App{
		dataDirectory: userDataDirectory,
		proxyManager:  func(data string) proxy.Manager { return proxy.Manager{Data: data} },
		lifecycleManager: func(data string) lifecycle.Manager {
			return lifecycle.Manager{Data: data, Processes: services.Store{Data: data}}
		},
	}
	a.commands = []Command{
		{Name: "init", Description: "Create project configuration in the current directory without overwriting existing files.", Options: []Option{
			{Name: "profile", Description: "Project profile identifier.", Required: true, Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128},
		}, Output: changedItemOutput(object(map[string]any{"config_path": stringSchema(), "profile": stringSchema()}, "config_path", "profile")), Run: func(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
			result, err := project.Init(".", request.Options["profile"])
			return map[string]any{"item": map[string]string{"config_path": result.ConfigPath, "profile": result.Profile}, "changed": result.Created}, err
		}},
		{Name: "help", Description: "Describe commands and their options.", Options: []Option{}, Output: map[string]any{"$ref": "#/$defs/catalog"}, Run: func(context.Context, IO, Request) (any, *protocol.Error) { return a.catalog(), nil }},
		{Name: "version", Description: "Report build and protocol versions.", Options: []Option{}, Output: object(map[string]any{
			"version":          stringSchema(),
			"commit":           stringSchema(),
			"protocol_version": map[string]any{"type": "integer", "const": protocol.ProtocolVersion},
		}, "version", "commit", "protocol_version"), Run: func(context.Context, IO, Request) (any, *protocol.Error) {
			return map[string]any{"version": version, "commit": commit, "protocol_version": protocol.ProtocolVersion}, nil
		}},
		{Name: "schema", Description: "Describe the machine interface using JSON Schema.", Options: []Option{}, Output: map[string]any{"$ref": "#/$defs/catalog"}, Run: func(context.Context, IO, Request) (any, *protocol.Error) { return a.catalog(), nil }},
		{Name: "project inspect", Description: "Resolve a project profile and user data paths without reading secrets.", Options: []Option{
			{Name: "dir", Description: "Directory to search from.", Default: ".", MinLength: 1},
			{Name: "profile", Description: "Explicit profile; bypasses configuration lookup.", Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128},
		}, Output: object(map[string]any{
			"item":  object(map[string]any{"profile": stringSchema(), "source": map[string]any{"enum": []string{"flag", "file"}}, "config_path": stringSchema(), "root": stringSchema()}, "profile", "source"),
			"paths": object(map[string]any{"config": stringSchema(), "data": stringSchema(), "cache": stringSchema()}, "config", "data", "cache"),
		}, "item", "paths"), Run: inspect},
	}
	a.registerTools()
	a.registerCommands()
	a.registerSkills()
	a.registerGuidance()
	a.registerImport()
	a.registerTasks()
	a.registerDashboard()
	a.registerDoctor()
	a.registerDiagnostics()
	a.registerPorts()
	a.registerProxy()
	a.registerBackup()
	a.registerProfiles()
	a.registerProcesses()
	a.registerProjectLifecycle()
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
				a.commands[i].OutputMode = OutputText
			}
		}
	}
	for i := range a.commands {
		if a.commands[i].OutputMode == "" {
			a.commands[i].OutputMode = OutputJSON
		}
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
		Item  project.Context   `json:"item"`
		Paths paths.Directories `json:"paths"`
	}{p, dirs}, nil
}

func (a *App) Run(ctx context.Context, args []string, streams IO) int {
	if len(args) == 1 && args[0] == "__complete" {
		return a.completeDynamic(ctx, streams)
	}
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
	if selected.OutputMode == OutputPassthrough {
		if result, ok := data.(processResult); ok {
			return result.ExitCode
		}
		return fail(protocol.NewError("internal_error", "Process command did not return an exit status.", 1, nil))
	}
	if ctx.Err() != nil {
		return fail(protocol.NewError("canceled", "Execution canceled.", 130, nil))
	}
	if selected.OutputMode == OutputText || selected.OutputMode == OutputArtifact {
		if data != nil {
			return fail(protocol.NewError("internal_error", "Command output does not match its declared mode.", 1, nil))
		}
		return 0
	}
	_, processOutput := data.(processResult)
	if selected.OutputMode != OutputJSON || processOutput {
		return fail(protocol.NewError("internal_error", "Command output does not match its declared mode.", 1, nil))
	}
	if protocol.Success(streams.Out, data) != nil {
		return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
	}
	return 0
}
