package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/dashboard"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"path/filepath"
)

func (a *App) registerDashboard() {
	for _, action := range []string{"start", "status", "stop"} {
		action := action
		name := "dashboard " + action
		aliases := []string{}
		if action == "start" {
			name = "dashboard"
			aliases = []string{"dashboard start"}
		}
		a.commands = append(a.commands, Command{Name: name, Aliases: aliases, Description: "Manage the local D3 Canvas dashboard and editing session.", Options: taskOptions(), Output: map[string]any{"type": "object"}, Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			dirs, e := paths.Current()
			if e != nil {
				return nil, argumentError("Cannot resolve user directories.", "")
			}
			dir, err := a.dataDirectory()
			if err != nil {
				return nil, err
			}
			profile := r.Options["profile"]
			if action == "start" {
				p, err := project.Resolve(".", profile)
				if err == nil {
					profile = p.Profile
				} else if profile != "" || err.Code != "project_not_found" {
					return nil, err
				}
			}
			return dashboard.Manage(ctx, action, dirs.Cache, filepath.Join(filepath.Dir(dir), "tasks"), profile)
		}})
	}
}
