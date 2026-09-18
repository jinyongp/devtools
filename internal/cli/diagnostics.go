package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/diagnostics"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/protocol"
)

func diagnosticsSectionSchema(fields map[string]any, required ...string) map[string]any {
	fields["status"] = map[string]any{"enum": []string{"pass", "fail"}}
	fields["error_code"] = stringSchema()
	return object(fields, append([]string{"status"}, required...)...)
}

func diagnosticsOutputSchema() map[string]any {
	profile := object(map[string]any{
		"profile":              stringSchema(),
		"values":               map[string]any{"type": "boolean"},
		"tasks":                map[string]any{"type": "boolean"},
		"env_count":            map[string]any{"type": "integer", "minimum": 0},
		"instance_count":       map[string]any{"type": "integer", "minimum": 0},
		"process_count":        map[string]any{"type": "integer", "minimum": 0},
		"active_process_count": map[string]any{"type": "integer", "minimum": 0},
	}, "profile", "values", "tasks", "env_count", "instance_count", "process_count", "active_process_count")
	pathsSchema := object(map[string]any{
		"data":   stringSchema(),
		"config": stringSchema(),
		"cache":  stringSchema(),
	}, "data", "config", "cache")
	profiles := diagnosticsSectionSchema(map[string]any{
		"items": map[string]any{"type": "array", "items": profile},
	}, "items")
	ports := diagnosticsSectionSchema(map[string]any{
		"instances":         map[string]any{"type": "integer", "minimum": 0},
		"assignments":       map[string]any{"type": "integer", "minimum": 0},
		"reservations":      map[string]any{"type": "integer", "minimum": 0},
		"missing_instances": map[string]any{"type": "integer", "minimum": 0},
		"unknown_locations": map[string]any{"type": "integer", "minimum": 0},
	}, "instances", "assignments", "reservations", "missing_instances", "unknown_locations")
	processes := diagnosticsSectionSchema(map[string]any{
		"total":       map[string]any{"type": "integer", "minimum": 0},
		"active":      map[string]any{"type": "integer", "minimum": 0},
		"ended":       map[string]any{"type": "integer", "minimum": 0},
		"running":     map[string]any{"type": "integer", "minimum": 0},
		"failed":      map[string]any{"type": "integer", "minimum": 0},
		"unknown":     map[string]any{"type": "integer", "minimum": 0},
		"interrupted": map[string]any{"type": "integer", "minimum": 0},
	}, "total", "active", "ended", "running", "failed", "unknown", "interrupted")
	proxy := diagnosticsSectionSchema(map[string]any{
		"running": map[string]any{"type": "boolean"},
		"state":   stringSchema(),
		"port":    map[string]any{"type": "integer", "minimum": 0, "maximum": 65535},
		"url":     stringSchema(),
		"reason":  stringSchema(),
	}, "running", "state", "port", "url", "reason")
	dashboard := diagnosticsSectionSchema(map[string]any{
		"running": map[string]any{"type": "boolean"},
	}, "running")
	backup := diagnosticsSectionSchema(map[string]any{
		"configured": map[string]any{"type": "boolean"},
		"directory":  stringSchema(),
	}, "configured", "directory")
	issue := object(map[string]any{
		"section": stringSchema(),
		"code":    stringSchema(),
	}, "section", "code")
	return object(map[string]any{
		"ready":     map[string]any{"type": "boolean"},
		"paths":     pathsSchema,
		"profiles":  profiles,
		"ports":     ports,
		"processes": processes,
		"proxy":     proxy,
		"dashboard": dashboard,
		"backup":    backup,
		"issues":    map[string]any{"type": "array", "items": issue},
	}, "ready", "paths", "profiles", "ports", "processes", "proxy", "dashboard", "backup", "issues")
}

func (a *App) registerDiagnostics() {
	a.commands = append(a.commands, Command{
		Name:        "diagnostics",
		Description: "Summarize local devtools subsystem health without returning secrets, raw logs, or credentials.",
		Output:      diagnosticsOutputSchema(),
		Run: func(ctx context.Context, _ IO, _ Request) (any, *protocol.Error) {
			dirs, err := paths.Current()
			if err != nil {
				return nil, protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
			}
			profilesDirectory, failure := a.dataDirectory()
			if failure != nil {
				return nil, failure
			}
			return diagnostics.Inspect(ctx, diagnostics.Input{
				Data:   filepath.Dir(profilesDirectory),
				Config: dirs.Config,
				Cache:  dirs.Cache,
			})
		},
	})
}
