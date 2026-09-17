package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
)

func (a *App) serviceStore() (services.Store, *protocol.Error) {
	d, e := a.dataDirectory()
	return services.Store{Data: filepath.Dir(d)}, e
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
			c.Arguments = []Argument{{Name: "command", Required: true, Pattern: project.ProfilePattern}}
			c.Options = append(c.Options, Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory."})
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
			if action == "start" {
				q.Command = r.Args[0]
				q.Directory = r.Options["dir"]
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
			return s.Apply(ctx, q)
		}
		a.commands = append(a.commands, c)
	}
}

func ServeProcess(ctx context.Context, data, id string) error {
	s := services.Store{Data: data}
	return s.Serve(ctx, id, func(ctx context.Context, r services.Record, runner process.Runner, output io.Writer) *protocol.Error {
		p, e := project.Resolve(r.Directory, "")
		if e != nil {
			return e
		}
		if p.Root != r.Directory || p.Profile != r.Profile {
			return protocol.NewError("instance_conflict", "Project identity changed.", 3, nil)
		}
		if os.Chdir(r.Directory) != nil {
			return protocol.NewError("io_error", "Cannot access execution directory.", 1, nil)
		}
		a := New("dev", "unknown")
		a.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(data, "profiles"), nil }
		options := map[string]string{}
		if r.EnvOverride != nil {
			options["env"] = *r.EnvOverride
		}
		_, e = a.runCommand(process.WithRunner(ctx, runner), IO{Out: output, Err: output}, Request{Args: []string{r.Command}, Options: options})
		return e
	})
}
