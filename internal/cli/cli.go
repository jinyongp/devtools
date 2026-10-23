// Package cli owns command registration, validation, and execution.
package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/lifecycle"
	"github.com/jinyongp/devtools/internal/notices"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/vault"
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
	Examples    []string                                                  `json:"examples"`
	BodyExample any                                                       `json:"body_example,omitempty"`
	BodySchema  map[string]any                                            `json:"body_schema,omitempty"`
	Aliases     []string                                                  `json:"aliases"`
	Arguments   []Argument                                                `json:"arguments"`
	ChildArgs   bool                                                      `json:"accepts_child_args"`
	UniqueArgs  bool                                                      `json:"-"`
	OutputMode  OutputMode                                                `json:"output_mode"`
	InputOneOf  []map[string]any                                          `json:"-"`
	InputAllOf  []map[string]any                                          `json:"-"`
	Name        string                                                    `json:"name"`
	Description string                                                    `json:"description"`
	Options     []Option                                                  `json:"options"`
	Output      map[string]any                                            `json:"output_schema"`
	Run         func(context.Context, IO, Request) (any, *protocol.Error) `json:"-"`
}

type App struct {
	vaultClient      func(context.Context, string) (*vault.SessionClient, error)
	userPaths        func() (paths.Directories, error)
	commands         []Command
	passphrasePrompt func(context.Context, IO, string) (string, *protocol.Error)
	dataDirectory    func() (string, *protocol.Error)
	proxyManager     func(string) proxy.Manager
	portProbe        ports.AvailabilityProbe
	lifecycleManager func(string) lifecycle.Manager
	isolatedRoot     string
}

type Argument struct {
	Name       string `json:"name"`
	Required   bool   `json:"required"`
	Repeatable bool   `json:"repeatable,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
}
type Request struct {
	Invocation    protocol.InvocationContext
	OptionOffsets map[string]int
	Options       map[string]string
	ListOptions   map[string][]string
	Args          []string
	Child         []string
	Help          bool
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
		passphrasePrompt: readTerminalPassphrase,
		dataDirectory:    userDataDirectory,
		proxyManager:     func(data string) proxy.Manager { return proxy.Manager{Data: data} },
		lifecycleManager: func(data string) lifecycle.Manager {
			return lifecycle.Manager{Data: data, Processes: services.Store{Data: data}}
		},
	}
	a.commands = []Command{
		{Name: "license", Description: "Print third-party license notices included in this standalone executable.", OutputMode: OutputArtifact, Run: func(_ context.Context, streams IO, _ Request) (any, *protocol.Error) {
			if _, err := io.WriteString(streams.Out, notices.Text); err != nil {
				return nil, protocol.NewError("io_error", "Cannot write license notices.", 1, nil)
			}
			return nil, nil
		}},
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
			"paths": object(map[string]any{"config": stringSchema(), "data": stringSchema(), "cache": stringSchema(), "runtime": stringSchema()}, "config", "data", "cache"),
		}, "item", "paths"), Run: a.inspect},
	}
	a.registerVault()
	a.registerTools()
	a.registerCommands()
	a.registerConfig()
	a.registerImport()
	a.registerBundles()
	a.registerValueUsage()
	a.registerValueExecutions()
	a.registerValueRunners()
	a.registerTasks()
	a.registerDashboard()
	a.registerDoctor()
	a.registerDiagnostics()
	a.registerPorts()
	a.registerProxy()
	a.registerCA()
	a.registerBackup()
	a.registerProfiles()
	a.registerProcesses()
	a.registerProjectLifecycle()
	a.registerCleanup()
	a.registerUpdate()
	a.registerCompletion()
	for i := range a.commands {
		a.commands[i].Options = append(a.commands[i].Options, Option{Name: "isolated-root", MinLength: 1, Description: "Use independent config/data/cache/runtime; shared OS mutations are prohibited."})
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
		a.commands[i].Examples = commandExamples(a.commands[i])
		if a.commands[i].BodySchema != nil {
			a.commands[i].BodyExample = bodyExample(a.commands[i])
		}
	}
	return a
}

func (a *App) inspect(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	options := request.Options
	p, err := project.Resolve(options["dir"], options["profile"])
	if err != nil {
		return nil, err
	}
	dirs, pathErr := a.currentPaths()
	if pathErr != nil {
		return nil, directoryError(pathErr)
	}
	return struct {
		Item  project.Context   `json:"item"`
		Paths paths.Directories `json:"paths"`
	}{p, dirs}, nil
}

func (a *App) Run(ctx context.Context, args []string, streams IO) int {
	ctx = doctor.WithCache(ctx)
	if len(args) == 1 && args[0] == "__complete" {
		return a.completeDynamic(ctx, streams)
	}
	operation := "devtools"
	rootArgumentOffset := 0
	fail := func(err *protocol.Error) int {
		if sources, ok := err.Details["sources"].([]any); ok && rootArgumentOffset > 0 {
			for _, value := range sources {
				if source, ok := value.(map[string]any); ok && source["kind"] == "argv" {
					if offset, ok := source["offset"].(int); ok {
						source["offset"] = offset + rootArgumentOffset
					}
				}
			}
		}
		err = protocol.Public(err, operation)
		if protocol.Failure(streams.Err, err) != nil {
			return 1
		}
		return err.ExitCode
	}
	if e := protocol.ContextError(ctx.Err()); e != nil {
		return fail(e)
	}
	rootPrefix := ""
	if len(args) > 0 && (args[0] == "--isolated-root" || strings.HasPrefix(args[0], "--isolated-root=")) {
		if args[0] == "--isolated-root" {
			if len(args) < 3 || args[1] == "" {
				return fail(argumentError("Supply an isolation root and command.", "isolated-root"))
			}
			rootPrefix = args[1]
			rootArgumentOffset = 2
			args = args[2:]
		} else {
			rootPrefix = strings.TrimPrefix(args[0], "--isolated-root=")
			rootArgumentOffset = 1
			args = args[1:]
			if rootPrefix == "" || len(args) == 0 {
				return fail(argumentError("Supply an isolation root and command.", "isolated-root"))
			}
		}
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
			if len(args) >= len(words) && strings.Join(args[:len(words)], " ") == name && (selected == nil || len(words) > len(args)-len(rest)) {
				selected, rest = &a.commands[i], args[len(words):]
			}
		}
	}
	if args[0] == "help" || args[0] == "schema" || (selected == nil && (args[len(args)-1] == "--help" || args[len(args)-1] == "-h") && !strings.Contains(strings.Join(args[:len(args)-1], " "), "--")) {
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
	if selected == nil {
		return fail(a.unknownCommand(args))
	}
	operation = selected.Name
	request, parseErr := parseRequest(*selected, rest)
	if parseErr != nil {
		if sources, ok := parseErr.Details["sources"].([]any); ok {
			for _, s := range sources {
				source := s.(map[string]any)
				if offset, ok := source["offset"].(int); ok {
					source["offset"] = offset + len(args) - len(rest)
				}
			}
		}
		return fail(parseErr)
	}
	for name, offset := range request.OptionOffsets {
		request.OptionOffsets[name] = offset + len(args) - len(rest) + rootArgumentOffset
	}
	if request.Help {
		_, text, _ := a.discovery(selected.Name, false)
		if writeHelp(streams.Out, text) != nil {
			return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
		}
		return 0
	}
	if err := platformCommand(selected.Name); err != nil {
		return fail(err)
	}
	if rootPrefix != "" && request.Options["isolated-root"] != "" {
		return fail(argumentError("Specify the isolation root once.", "isolated-root"))
	}
	root := request.Options["isolated-root"]
	if root == "" {
		root = rootPrefix
	}
	if root != "" {
		dirs, err := paths.Isolated(root)
		if err != nil {
			return fail(protocol.WithStorageDetails(argumentError("Cannot resolve isolation root.", "isolated-root"), "paths", "resolve_isolation_root", "isolated_root", err))
		}
		oldRoot, oldDirectory := a.isolatedRoot, a.dataDirectory
		a.isolatedRoot = dirs.IsolatedRoot
		a.dataDirectory = func() (string, *protocol.Error) { return dirs.Data + "/profiles", nil }
		defer func() { a.isolatedRoot = oldRoot; a.dataDirectory = oldDirectory }()
	}
	request.Invocation.Operation = selected.Name
	request.Invocation.Directory, _ = os.Getwd()
	if dirs, e := a.currentPaths(); e == nil {
		request.Invocation.IsolatedRoot = dirs.IsolatedRoot
	}
	request.Invocation.RootSource = "default"
	if root != "" {
		request.Invocation.RootSource = "flag"
	} else if request.Invocation.IsolatedRoot != "" {
		request.Invocation.RootSource = "env"
	}
	data, err := selected.Run(ctx, streams, request)
	if err != nil {
		if err.Details == nil {
			err.Details = map[string]any{}
		}
		if _, ok := err.Details["target"]; !ok {
			target := map[string]any{"kind": strings.ReplaceAll(strings.Fields(selected.Name)[0], "-", "_"), "name": selected.Name}
			if len(request.Args) > 0 {
				target["name"] = request.Args[0]
			}
			for _, key := range []string{"profile", "env", "bundle", "dir"} {
				if value, ok := request.Options[key]; ok {
					field := key
					if key == "dir" {
						field = "directory"
					}
					target[field] = value
				}
			}
			err.Details["target"] = target
		}
		if id := request.Options["request-id"]; id != "" {
			err.Details["request_id"] = id
		}
		err.Details = protocol.BindGuidance(err.Details, request.Invocation).(map[string]any)
		return fail(err)
	}
	if selected.OutputMode == OutputPassthrough {
		if result, ok := data.(processResult); ok {
			return result.ExitCode
		}
		return fail(protocol.NewError("internal_error", "Process command did not return an exit status.", 1, nil))
	}
	// A successful handler owns its commit outcome; cancellation arriving after
	// publication must not replace that result with an ambiguous failure.
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
	if protocol.Success(streams.Out, protocol.BindGuidance(data, request.Invocation)) != nil {
		return fail(protocol.NewError("io_error", "Cannot write output.", 1, nil))
	}
	return 0
}
