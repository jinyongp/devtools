package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"

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
	for _, action := range []string{"start", "list", "status", "stop", "restart", "logs"} {
		c := Command{Name: "process " + action, Description: action + " managed project processes.", Options: []Option{}, Output: map[string]any{"type": "object"}}
		if action == "list" {
			c.Options = profileOptions(false)
		} else {
			c.Arguments = []Argument{{Name: "id", Required: true}}
		}
		if action == "start" {
			c.Arguments = []Argument{{Name: "command", Required: true, Pattern: project.ProfilePattern}}
			c.Options = append(c.Options, Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory."})
		}
		if action == "start" || action == "restart" {
			c.Options = append(c.Options, Option{Name: "env", Pattern: project.ProfilePattern, MinLength: 1, Description: "Inject this environment."}, Option{Name: "capture-logs", Boolean: true, Description: "Retain bounded raw output for seven days after exit."})
		}
		if action == "start" || action == "stop" || action == "restart" {
			c.Options = append(c.Options, Option{Name: "request-id", Required: true, MinLength: 36, MaxLength: 36, Description: "UUID for retry-safe mutation."})
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
				return s.Status(ctx, r.Args[0])
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
