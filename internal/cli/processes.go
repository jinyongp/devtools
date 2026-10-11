package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
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
	return services.Store{Data: filepath.Dir(d), IsolatedRoot: directories.IsolatedRoot}, nil
}
func (a *App) registerProcesses() {
	descriptions := map[string]string{
		"start":   "Start a configured command as a managed execution.",
		"list":    "List managed executions.",
		"status":  "Show one managed execution.",
		"stop":    "Stop one managed execution.",
		"restart": "Replace one managed execution using current configuration.",
		"logs":    "Read retained output for one managed execution.",
		"check":   "Check readiness once for one managed execution.",
		"wait":    "Wait for one managed execution to become ready.",
	}
	for _, action := range []string{"start", "list", "status", "stop", "restart", "logs", "check", "wait"} {
		c := Command{Name: "process " + action, Description: descriptions[action], Options: []Option{}, Output: map[string]any{"type": "object"}}
		if action == "list" {
			c.Options = profileOptions(false)
		} else {
			c.Arguments = []Argument{{Name: "execution-id", Required: true, Pattern: uuidPattern}}
		}
		if action == "start" {
			c.Arguments = []Argument{{Name: "command", Required: true, Pattern: project.CommandPattern}}
			c.Options = append(c.Options, Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory."})
			c.Options = append(c.Options, Option{Name: "proxy", Boolean: true, Description: "Prepare the declared route and DNS before starting."})
		}
		if action == "start" || action == "restart" {
			c.Options = append(c.Options, Option{Name: "env", Pattern: project.ProfilePattern, MinLength: 1, Description: "Inject this environment."}, Option{Name: "capture-logs", Boolean: true, Description: "Retain bounded raw output for seven days after exit."})
		}
		if action == "wait" {
			c.Options = append(c.Options, Option{Name: "timeout", Default: "30s", Description: "Overall readiness wait, greater than zero and at most 10m."})
		}
		if action == "check" || action == "wait" {
			c.Output = map[string]any{"type": "object", "required": []string{"id", "ready_configured", "readiness"}, "properties": map[string]any{
				"id": map[string]any{"type": "string"}, "ready_configured": map[string]any{"type": "boolean"},
				"readiness": map[string]any{"type": "object", "required": []string{"ready", "checked_at", "reason", "exit_code"}, "properties": map[string]any{
					"ready": map[string]any{"type": "boolean"}, "checked_at": map[string]any{"type": "string", "format": "date-time"}, "reason": map[string]any{"type": "string"}, "exit_code": map[string]any{"type": []string{"integer", "null"}},
				}},
			}}
		}
		if action == "start" || action == "stop" || action == "restart" {
			c.Output = object(map[string]any{"item": map[string]any{"type": "object"}, "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "item", "changed", "replayed")
			c.Options = append(c.Options, Option{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe mutation."})
		}
		if action == "list" {
			c.Output = object(map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}, "items")
		} else if action == "status" {
			c.Output = itemOutput(map[string]any{"type": "object"})
		} else if action == "logs" {
			c.Output = object(map[string]any{"id": stringSchema(), "content": stringSchema()}, "id", "content")
		}
		c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			s, e := a.serviceStore()
			if e != nil {
				return nil, e
			}
			if action == "list" {
				items, e := s.List(ctx, r.Options["profile"])
				return map[string]any{"items": items}, e
			}
			if action == "status" {
				item, err := s.Status(ctx, r.Args[0])
				return map[string]any{"item": item}, err
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
				p, resolveErr := project.Resolve(r.Options["dir"], "")
				if resolveErr != nil {
					return nil, resolveErr
				}
				startProject = p
				q.Command = r.Args[0]
				q.Directory = p.Root
			} else {
				q.ID = r.Args[0]
			}
			if env, ok := r.Options["env"]; ok {
				q.Env = &env
			}
			if capture, ok := r.Options["capture-logs"]; ok {
				v := capture == "true"
				q.Capture = &v
			}
			if action == "start" {
				command, env := q.Command, q.Env
				if r.Options["proxy"] == "true" {
					snapshots, e := a.prepareProxyNetwork(ctx, startProject, []string{command}, nil, map[string]*string{command: env}, streams, q.RequestID)
					if e != nil {
						return nil, e
					}
					q.ProxyMode = true
					q.Network = snapshots[command]
				}
				q.BeforeStart = func(ctx context.Context) *protocol.Error {
					if q.Network != nil {
						return a.preflightNetworkSnapshot(ctx, startProject, command, env, q.Network, false)
					}
					return a.preflightProjectCommand(ctx, startProject, command, env)
				}
			}
			if action == "restart" {
				original, e := s.Status(ctx, q.ID)
				if e != nil {
					return nil, e
				}
				if original.ProxyMode {
					p, e := project.Resolve(original.Directory, "")
					if e != nil {
						return nil, e
					}
					env := q.Env
					if env == nil {
						env = original.EnvOverride
					}
					snapshots, e := a.prepareProxyNetwork(ctx, p, []string{original.Command}, []services.Record{original}, map[string]*string{original.Command: env}, streams, q.RequestID)
					if e != nil {
						return nil, e
					}
					q.ProxyMode = true
					q.Network = snapshots[original.Command]
				}
				envOverride := q.Env
				q.RestartPreflight = func(ctx context.Context, record services.Record, phase services.RestartPhase) *protocol.Error {
					env := envOverride
					if env == nil {
						env = record.EnvOverride
					}
					snapshot := project.Context{Root: record.Directory, Profile: record.Profile}
					if q.Network != nil {
						return a.preflightNetworkSnapshot(ctx, snapshot, record.Command, env, q.Network, phase == services.RestartBeforeStop)
					}
					switch phase {
					case services.RestartBeforeStop:
						return a.validateProjectCommand(ctx, snapshot, record.Command, env)
					case services.RestartBeforeStart:
						return a.preflightProjectCommand(ctx, snapshot, record.Command, env)
					default:
						return protocol.NewError("internal_error", "Invalid restart preflight phase.", 1, nil)
					}
				}
			}
			return s.Apply(ctx, q)
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
		p, err := project.Resolve(r.Directory, "")
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
		dependencies, err := execution.DependenciesFor(command, data, configRoot)
		if err != nil {
			return err
		}
		prepared, err := execution.Prepare(ctx, command, dependencies, false)
		if err != nil {
			return err
		}
		defer prepared.Release()
		checks := prepared.Checks(ctx, os.Environ(), false)
		if !execution.ChecksPassed(checks) {
			return protocol.NewError("requirements_failed", "Command prerequisites are not satisfied. Run devtools doctor for diagnostics.", 3, map[string]any{"checks": checks})
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
