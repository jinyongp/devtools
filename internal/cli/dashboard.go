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
		options := taskOptions()
		fields := map[string]any{"running": map[string]any{"type": "boolean"}, "server_id": map[string]any{"type": []string{"string", "null"}}}
		output := itemOutput(fieldsSchema(fields))
		if action == "start" {
			fields["url"] = stringSchema()
			fields["initial_profile"] = stringSchema()
			output = changedItemOutput(fieldsSchema(fields))
		} else if action == "stop" {
			output = object(map[string]any{"changed": map[string]any{"type": "boolean"}}, "changed")
		}
		a.commands = append(a.commands, Command{Name: name, Aliases: aliases, Description: "Manage the local D3 Canvas dashboard and editing session.", Options: options, Output: output, Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
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
			result, err := dashboard.Manage(ctx, action, dirs.Cache, filepath.Join(filepath.Dir(dir), "tasks"), profile)
			if err != nil {
				return nil, err
			}
			if action == "stop" {
				return map[string]any{"changed": result["stopped"]}, nil
			}
			if action == "start" {
				// Starting also issues a fresh dashboard login link when reusing a server.
				return map[string]any{"item": result, "changed": true}, nil
			}
			return map[string]any{"item": result}, nil
		}})
	}
}
