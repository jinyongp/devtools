package execution

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

// Dependencies supplies user-global stores needed to prepare one command.
// Callers keep ownership of user path/config discovery.
type Dependencies struct {
	ValuesDirectory string
	Ports           ports.Store
	PortDefaults    []int
}

// DependenciesFor derives the stores required by one effective command from the
// user-global data/config roots without creating or mutating storage.
func DependenciesFor(command Command, dataRoot, configRoot string) (Dependencies, *protocol.Error) {
	dependencies := Dependencies{}
	if command.NeedsValues() {
		dependencies.ValuesDirectory = filepath.Join(dataRoot, "profiles")
	}
	if len(command.Bind)+len(command.Serve) > 0 {
		defaults, err := ports.DefaultRange(filepath.Join(configRoot, "config.toml"))
		if err != nil {
			return Dependencies{}, err
		}
		dependencies.Ports = ports.Store{Directory: filepath.Join(dataRoot, "ports")}
		dependencies.PortDefaults = defaults
	}
	return dependencies, nil
}

// Prepared contains the side-effect-bounded result of command preparation.
// Release must be called after non-preview port preparation.
type Prepared struct {
	Command      Command
	Args         []string
	Injected     map[string]string
	State        *values.State
	PathOverride *string
	Release      func()
}

// NeedsValues reports whether preparing the command must inspect profile values.
func (c Command) NeedsValues() bool {
	return c.Inject || len(c.Requirements.Vars)+len(c.Requirements.Secs) > 0 || len(c.Bind) > 0
}

// Prepare loads values, applies the selected environment, resolves bindings,
// and optionally claims served ports.
func Prepare(ctx context.Context, command Command, dependencies Dependencies, preview bool) (Prepared, *protocol.Error) {
	var state *values.State
	if command.NeedsValues() {
		var err *protocol.Error
		state, err = (values.Store{Directory: dependencies.ValuesDirectory, Profile: command.Project.Profile}).Read(ctx)
		if err != nil {
			return Prepared{}, err
		}
		if preview {
			if envErr := state.CheckEnv(command.Env); envErr != nil {
				return Prepared{}, envErr
			}
		}
	}
	return prepareResolved(ctx, command, state, dependencies, preview, !preview)
}

// Preview resolves diagnostic bindings and ports from an already observed value
// snapshot without injecting that snapshot into a child environment. A nil state
// is valid so doctor can continue after reporting unreadable value storage.
func Preview(ctx context.Context, command Command, state *values.State, dependencies Dependencies) (Prepared, *protocol.Error) {
	return prepareResolved(ctx, command, state, dependencies, true, false)
}

func prepareResolved(ctx context.Context, command Command, state *values.State, dependencies Dependencies, preview, injectValues bool) (Prepared, *protocol.Error) {
	out := Prepared{
		Command:  command,
		Args:     command.Arguments(),
		Injected: map[string]string{},
		State:    state,
		Release:  func() {},
	}
	if injectValues && command.Inject && state != nil {
		injected, envErr := state.Environment(command.Env)
		if envErr != nil {
			return Prepared{}, envErr
		}
		out.Injected = injected
	}
	if len(command.Bind)+len(command.Serve) == 0 {
		return out, nil
	}
	prepared, release, err := dependencies.Ports.Prepare(ctx, command.Project, command.projectCommand(), command.Env, state, dependencies.PortDefaults, preview)
	if err != nil {
		return Prepared{}, err
	}
	out.Release = release
	out.Args = append(append([]string{}, prepared.Args...), command.ExtraArgs...)
	for key, value := range prepared.Bind {
		out.Injected[key] = value
	}
	if path, ok := prepared.Bind["PATH"]; ok {
		path := path
		out.PathOverride = &path
	}
	return out, nil
}

// Checks applies the shared structured requirement diagnostics. forceExecutable
// preserves managed preflight behavior; foreground execution can leave it false
// so commands without declared requirements fail naturally at exec time.
func (p Prepared) Checks(ctx context.Context, parentEnvironment []string, forceExecutable bool) []doctor.Check {
	if p.Command.Requirements.Empty() && !forceExecutable {
		return []doctor.Check{}
	}
	executable := ""
	if len(p.Args) > 0 {
		executable = p.Args[0]
	}
	return doctor.CheckRequirements(ctx, doctor.Input{
		Profile:      p.Command.Project.Profile,
		Directory:    p.Command.Directory,
		Env:          p.Command.Env,
		Requirements: p.Command.Requirements,
		State:        p.State,
		Inject:       p.Command.Inject,
		Executable:   executable,
		Environment:  parentEnvironment,
		PathOverride: p.PathOverride,
	})
}

// Validate performs only checks that are safe while an existing managed
// execution is still running. It does not claim ports or require served ports
// to be free. Bindings backed by a new port served by this same command may be
// deferred until the post-stop cold-start preflight.
func Validate(ctx context.Context, command Command, dependencies Dependencies, parentEnvironment []string) *protocol.Error {
	var state *values.State
	if command.NeedsValues() {
		var err *protocol.Error
		state, err = (values.Store{Directory: dependencies.ValuesDirectory, Profile: command.Project.Profile}).Read(ctx)
		if err != nil {
			return err
		}
		if envErr := state.CheckEnv(command.Env); envErr != nil {
			return envErr
		}
	}

	args := command.Arguments()
	pendingBindings := map[string]bool{}
	executableDeferred := false
	var pathOverride *string
	if len(command.Bind)+len(command.Serve) > 0 {
		portState, err := dependencies.Ports.Read()
		if err != nil {
			return err
		}
		bindings, pending, bindErr := portState.BindValidation(command.Project, command.projectCommand(), command.Env, state)
		if bindErr != nil {
			return bindErr
		}
		pendingBindings = pending
		resolvedExec := make([]string, len(command.Exec))
		for index, argument := range command.Exec {
			deferred := false
			expanded, expandErr := project.Expand(argument, func(ref string) (string, bool) {
				if !strings.HasPrefix(ref, "bind.") {
					return "", false
				}
				name := strings.TrimPrefix(ref, "bind.")
				if pendingBindings[name] {
					deferred = true
					return "", true
				}
				value, ok := bindings[name]
				return value, ok
			})
			if expandErr != nil {
				return expandErr
			}
			if deferred {
				resolvedExec[index] = argument
				if index == 0 {
					executableDeferred = true
				}
			} else {
				resolvedExec[index] = expanded
			}
		}
		args = append(resolvedExec, command.ExtraArgs...)
		if path, ok := bindings["PATH"]; ok {
			path := path
			pathOverride = &path
		}
	}

	requirements := command.Requirements
	executable := ""
	if len(args) > 0 && !executableDeferred {
		executable = args[0]
	}
	if pendingBindings["PATH"] {
		absoluteTools := map[string]project.Tool{}
		for name, tool := range requirements.Tools {
			if filepath.IsAbs(tool.WithDefaults(name).Executable) {
				absoluteTools[name] = tool
			}
		}
		requirements.Tools = absoluteTools
		if executable != "" && !filepath.IsAbs(executable) {
			executable = ""
		}
		pathOverride = nil
	}
	checks := doctor.CheckRequirements(ctx, doctor.Input{
		Profile:      command.Project.Profile,
		Directory:    command.Directory,
		Env:          command.Env,
		Requirements: requirements,
		State:        state,
		Inject:       command.Inject,
		Executable:   executable,
		Environment:  parentEnvironment,
		PathOverride: pathOverride,
	})
	if !ChecksPassed(checks) {
		return protocol.NewError("requirements_failed", "Command prerequisites are not satisfied.", 3, map[string]any{
			"command": command.Name,
			"checks":  checks,
		})
	}
	return nil
}

// Preflight performs the non-mutating checks used before a managed cold start.
func Preflight(ctx context.Context, command Command, dependencies Dependencies, parentEnvironment []string) *protocol.Error {
	prepared, err := Prepare(ctx, command, dependencies, true)
	if err != nil {
		return err
	}
	defer prepared.Release()
	checks := prepared.Checks(ctx, parentEnvironment, true)
	if !ChecksPassed(checks) {
		return protocol.NewError("requirements_failed", "Command prerequisites are not satisfied.", 3, map[string]any{
			"command": command.Name,
			"checks":  checks,
		})
	}
	return nil
}

// Environment overlays injected profile and binding values on the caller's
// process environment.
func (p Prepared) Environment(parent []string) []string {
	return process.Environment(parent, p.Injected)
}

// Execute runs a prepared command using either the regular foreground process
// path or a caller-provided managed-process runner.
func (p Prepared) Execute(ctx context.Context, parentEnvironment []string, runner process.Runner, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	ctx = process.WithReadyProbe(ctx, p.Command.Ready)
	if runner != nil {
		ctx = process.WithRunner(ctx, runner)
	}
	return process.Execute(ctx, p.Args, p.Command.Directory, p.Environment(parentEnvironment), in, out, diagnostic)
}

// ChecksPassed reports whether every structured check succeeded.
func ChecksPassed(checks []doctor.Check) bool {
	for _, check := range checks {
		if check.Status != "pass" {
			return false
		}
	}
	return true
}
