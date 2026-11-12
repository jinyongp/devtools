package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func (a *App) serviceStore() (services.Store, *protocol.Error) {
	d, e := a.dataDirectory()
	if e != nil {
		return services.Store{}, e
	}
	directories, err := a.currentPaths()
	if err != nil {
		return services.Store{}, portFailure("io_error")
	}
	return services.Store{Data: filepath.Dir(d), IsolatedRoot: directories.IsolatedRoot, PrepareInput: a.prepareManagedInput}, nil
}
func (a *App) registerProcesses() {
	descriptions := map[string]string{
		"start":   "Start a configured command as a managed execution.",
		"list":    "List current project services or explicit execution history.",
		"status":  "Show one execution, its control capabilities and owned-scope cleanup.",
		"stop":    "Stop one foreground or managed execution and verify owned-scope cleanup.",
		"restart": "Replace one execution with a managed execution using current configuration.",
		"logs":    "Read retained output for one managed execution.",
		"check":   "Check readiness once for one controlled execution.",
		"wait":    "Wait for one controlled execution to become ready.",
	}
	for _, action := range []string{"start", "list", "status", "stop", "restart", "logs", "check", "wait"} {
		c := Command{Name: "process " + action, Description: descriptions[action], Options: []Option{}, Output: map[string]any{"type": "object"}}
		if action == "list" {
			c.Options = profileOptions(false)
		} else {
			c.Arguments = []Argument{{Name: "execution-id", Required: true, Pattern: uuidPattern, Completion: completeProcess}}
		}
		if action == "start" {
			c.Arguments = []Argument{{Name: "command", Required: true, Pattern: project.CommandPattern, Completion: completeCommand}}
			c.Options = append(c.Options, Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory."})
			c.Description += " Declared served proxies prepare automatically."
			c.Options = append(c.Options, Option{Name: "proxy", Boolean: true, Description: "Require matching declared routes and prepare them, including disabled routes; false skips preparation."})
			c.Options = append(c.Options, Option{Name: "no-proxy", Boolean: true, Description: "Skip proxy preparation without stopping existing routes."})
		}
		if action == "start" || action == "restart" {
			c.Options = append(c.Options, Option{Name: "bundle", Pattern: project.ProfilePattern, Completion: completeSelection, Description: "Select all, none, or a shared value bundle; restart inherits the previous explicit override."}, Option{Name: "env", Pattern: project.ProfilePattern, Completion: completeEnv, MinLength: 1, Description: "Inject this environment."}, Option{Name: "capture-logs", Boolean: true, Description: "Retain bounded redacted output for seven days after exit."})
		}
		if action == "wait" {
			c.Options = append(c.Options, Option{Name: "timeout", Default: "30s", Description: "Overall readiness wait, greater than zero and at most 10m."})
		}
		if action == "check" || action == "wait" {
			c.Output = processExecutionSchema()
			c.Output["required"] = append(c.Output["required"].([]string), "readiness")
		}
		if action == "start" || action == "stop" || action == "restart" {
			c.Output = object(map[string]any{"item": processExecutionSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "item", "changed", "replayed")
			c.Options = append(c.Options, requestIDOption(uuidPattern))
		}
		if action == "list" {
			c.Output = object(map[string]any{"items": arraySchema(processNavigationSchema()), "values": map[string]any{"type": "object", "additionalProperties": inputObservationSchema()}}, "items", "values")
		} else if action == "status" {
			c.Output = object(map[string]any{"item": processExecutionSchema(), "values": inputObservationSchema()}, "item", "values")
		} else if action == "logs" {
			c.Output = object(map[string]any{"id": stringSchema(), "content": stringSchema()}, "id", "content")
		}
		c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			s, e := a.serviceStore()
			if e != nil {
				return nil, e
			}
			if action == "list" {
				selection := services.NavigationSelection{Profile: r.Options["profile"], Command: r.Options["command"], Mode: r.Options["mode"], Search: r.Options["query"], History: r.Options["view"] == "history"}
				if r.Scope != nil {
					selection.Profile, selection.Directory, selection.Instance = r.Scope.Profile, r.Scope.Directory, r.Scope.Instance
				}
				if _, explicit := r.OptionOffsets["env"]; explicit {
					value := r.Options["env"]
					selection.Env = &value
				}
				items, e := s.Navigation(ctx, selection, nil)
				if e != nil {
					return nil, e
				}
				return map[string]any{"items": items, "values": map[string]values.InputObservation{}}, nil
			}
			if action == "status" {
				item, err := s.Status(ctx, r.Args[0])
				if err != nil {
					return nil, err
				}
				return map[string]any{"item": item, "values": s.InspectRecordValues(ctx, item)}, nil
			}
			if action == "check" {
				return s.Check(ctx, r.Args[0])
			}
			if action == "wait" {
				timeout, err := time.ParseDuration(r.Options["timeout"])
				if err != nil || timeout <= 0 || timeout > 10*time.Minute {
					return nil, argumentError("Expected a positive duration up to 10m.", "timeout")
				}
				return s.Wait(ctx, r.Args[0], timeout)
			}
			if action == "logs" {
				content, e := s.Logs(ctx, r.Args[0])
				return map[string]any{"id": r.Args[0], "content": content}, e
			}
			q := services.Request{Action: action, RequestID: r.Options["request-id"]}
			var startProject project.Context
			if action == "start" {
				q.Command = r.Args[0]
				q.Directory = r.Options["dir"]
			} else {
				q.ID = r.Args[0]
			}
			if bundle, ok := r.Options["bundle"]; ok {
				q.Bundle = &bundle
			}
			if env, ok := r.Options["env"]; ok {
				q.Env = &env
			}
			if capture, ok := r.Options["capture-logs"]; ok {
				v := capture == "true"
				q.Capture = &v
			}
			mode := network.PrepareAuto
			if action == "start" {
				var e *protocol.Error
				mode, e = preparationMode(r.Options)
				if e != nil {
					return nil, e
				}
			}
			if out, recovered, e := s.RecoverCompleted(ctx, q, q.Directory, mode); recovered || e != nil {
				return out, e
			}
			if action == "start" {
				p, resolveErr := a.resolveStoredProject(r.Options["dir"], "")
				if resolveErr != nil {
					recorded, readErr := s.HasRequest(q.RequestID)
					if readErr != nil {
						return nil, readErr
					}
					if recorded {
						return nil, services.RequestRecoveryError("process", q.RequestID)
					}
					return nil, resolveErr
				}
				startProject, q.Directory = p, p.Root
				command, env := q.Command, q.Env
				if e := a.prepareManagedNetwork(ctx, startProject, &q, nil, mode, streams, r.Options["dir"]); e != nil {
					return nil, e
				}
				reuseCheck := q.BeforeStart
				q.BeforeStart = func(ctx context.Context) *protocol.Error {
					if reuseCheck != nil {
						if e := reuseCheck(ctx); e != nil {
							return e
						}
					}
					if q.Network != nil {
						return a.preflightSelectedNetworkSnapshot(ctx, startProject, command, env, q.Network, false, q.Bundle)
					}
					return a.checkSelectedProjectCommand(ctx, startProject, command, env, q.Bundle, false)
				}
			}
			if action == "restart" {
				original, e := s.Status(ctx, q.ID)
				if e != nil {
					return nil, e
				}
				if original.ProxyMode {
					p, e := a.resolveStoredProject(original.Directory, "")
					if e != nil {
						return nil, e
					}
					if e := a.prepareManagedNetwork(ctx, p, &q, &original, network.PrepareAuto, streams); e != nil {
						return nil, e
					}
				}
				envOverride := q.Env
				bundleOverride := q.Bundle
				q.RestartPreflight = func(ctx context.Context, record services.Record, phase services.RestartPhase) *protocol.Error {
					env := envOverride
					if env == nil {
						env = record.EnvOverride
					}
					bundle := bundleOverride
					if bundle == nil {
						bundle = record.BundleOverride
					}
					snapshot := project.Context{Root: record.Directory, Profile: record.Profile}
					if q.Network != nil {
						return a.preflightSelectedNetworkSnapshot(ctx, snapshot, record.Command, env, q.Network, phase == services.RestartBeforeStop, bundle)
					}
					switch phase {
					case services.RestartBeforeStop:
						return a.checkSelectedProjectCommand(ctx, snapshot, record.Command, env, bundle, true)
					case services.RestartBeforeStart:
						return a.checkSelectedProjectCommand(ctx, snapshot, record.Command, env, bundle, false)
					default:
						return protocol.NewError("internal_error", "Invalid restart preflight phase.", 1, nil)
					}
				}
			}
			return s.Apply(ctx, q)
		}
		if action == "start" || action == "restart" {
			run := c.Run
			c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
				value, condition := run(ctx, streams, r)
				if condition != nil && len(r.Args) > 0 {
					if store, e := a.serviceStore(); e == nil {
						q := services.Request{Action: action, RequestID: r.Options["request-id"]}
						if action == "start" {
							q.Directory, q.Command = r.Options["dir"], r.Args[0]
						} else {
							q.ID = r.Args[0]
						}
						if env, ok := r.Options["env"]; ok {
							q.Env = &env
						}
						_ = store.RecordPreparationFailure(ctx, q, condition)
					}
				}
				return value, condition
			}
		}
		a.commands = append(a.commands, c)
	}
}

func ServeProcess(ctx context.Context, data, id string) error {
	return serveProcess(ctx, data, id, nil)
}

func ServeProcessWithLease(ctx context.Context, data, id string, lease *os.File) error {
	return serveProcess(ctx, data, id, lease)
}

func serveProcess(ctx context.Context, data, id string, lease *os.File) error {
	s := services.Store{Data: data}
	execute := func(ctx context.Context, r services.Record, runner process.Runner, output io.Writer) *protocol.Error {
		p, err := profilekey.ResolveProject(data, r.Directory, "")
		if err != nil {
			return err
		}
		if p.Root != r.Directory || p.Profile != r.Profile {
			return protocol.NewError("instance_conflict", "Project identity changed.", 3, nil)
		}
		command, err := execution.ResolveConfigured(p, r.Command, r.EnvOverride, nil)
		if err != nil {
			return err
		}
		if r.BundleOverride != nil {
			command.Selection = *r.BundleOverride
		}
		if r.ProxyMode {
			var snapshot network.ExecutionSnapshot
			if tasks.ReadPrivate(filepath.Join(data, "processes", r.ID, "network.json"), &snapshot) != nil || snapshot.ID != r.NetworkSnapshotID {
				return protocol.NewError("network_snapshot_required", "The managed network snapshot is unavailable.", 3, nil)
			}
			command.Network = &snapshot
		}
		configRoot := ""
		if len(command.Bind)+len(command.Serve) > 0 {
			dirs, pathErr := paths.Current()
			if r.IsolatedRoot != "" {
				dirs, pathErr = paths.Isolated(r.IsolatedRoot)
			}
			if pathErr != nil {
				return protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
			}
			configRoot = dirs.Config
		}
		dependencies, err := (services.Store{Data: data, IsolatedRoot: r.IsolatedRoot}).ExecutionDependencies(command, configRoot)
		if err != nil {
			return err
		}
		dependencies.CaptureOutput = r.Capture
		dependencies.IsolatedRoot = r.IsolatedRoot
		command.Invocation.IsolatedRoot = r.IsolatedRoot
		prepared, err := execution.PrepareInput(ctx, command, dependencies, os.Environ(), execution.PlanOptions{Mode: "managed"})
		if err != nil {
			return err
		}
		defer prepared.Release()
		checks := prepared.Checks(ctx, os.Environ(), false)
		if !execution.ChecksPassed(checks) {
			return doctor.FailureFor(checks, command.Diagnostic("managed"))
		}
		ctx = services.WithEnvironmentSelection(ctx, command.Env, command.Inject)
		_, err = prepared.Execute(ctx, os.Environ(), runner, nil, output, output)
		return err
	}
	if lease != nil {
		return s.ServeWithLease(ctx, id, lease, execute)
	}
	return s.Serve(ctx, id, execute)
}

// prepareManagedInput performs all value/config/tool work before the launcher
// is admitted and before a restart stops its old execution.
func (a *App) prepareManagedInput(ctx context.Context, r services.Record, networkSnapshot *network.ExecutionSnapshot) (execution.Prepared, *protocol.Error) {
	profiles, failure := a.dataDirectory()
	if failure != nil {
		return execution.Prepared{}, failure
	}
	return services.PrepareFrozenInput(ctx, filepath.Dir(profiles), r, networkSnapshot, a.executionDependencies)
}

func ServePreparedProcessWithLease(ctx context.Context, data, id string, lease *os.File) error {
	store := services.Store{Data: data}
	return store.ServeWithLease(ctx, id, lease, func(ctx context.Context, r services.Record, runner process.Runner, output io.Writer) *protocol.Error {
		bound, e := services.ReceiveLauncherInput(ctx, os.Stdin, os.Stdout, r, data, output, output)
		if e != nil {
			services.ReportLauncherFailure(os.Stdout, e)
			return e
		}
		defer bound.Close()
		ctx = bound.Streams.Context
		input, e := bound.Payload.Open()
		if e != nil {
			return e
		}
		defer input.Destroy()
		portStore := store.Ports()
		portStore.Owner = ports.RunOwner{Kind: "managed", Command: r.Command, ExecutionID: r.ID}
		release, e := portStore.ClaimPrepared(ctx, input.Served)
		if e != nil {
			return e
		}
		defer release()
		metadata := bound.Payload.Metadata()
		ctx = services.WithEnvironmentSelection(ctx, metadata.Env, metadata.Inject)
		ctx = process.WithExecutablePath(process.WithReadyProbe(ctx, input.Ready), input.Executable)
		ctx = process.WithStarted(ctx, func() *protocol.Error { return bound.Complete("spawned") })
		_, e = runner(ctx, input.Args, input.Directory, input.Environment, nil, bound.Streams.Out, bound.Streams.Err)
		if outputErr := bound.FinishOutput(ctx.Err() != nil || e != nil); outputErr != nil {
			return outputErr
		}
		return e
	})
}

func selectedBundleOverride(command string, bundle *string) map[string]string {
	selected := map[string]string{}
	if bundle != nil {
		selected[command] = *bundle
	}
	return selected
}
