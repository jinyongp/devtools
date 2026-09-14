package cli

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
)

func proxyStatusSchema() map[string]any {
	return object(map[string]any{
		"running":    map[string]any{"type": "boolean"},
		"state":      stringSchema(),
		"port":       map[string]any{"type": "integer", "minimum": 0, "maximum": 65535},
		"url":        stringSchema(),
		"started_at": map[string]any{"type": []string{"string", "null"}, "format": "date-time"},
		"reason":     stringSchema(),
		"changed":    map[string]any{"type": "boolean"},
		"replayed":   map[string]any{"type": "boolean"},
	}, "running", "state", "port", "url", "started_at", "reason")
}

func proxyItemSchema() map[string]any {
	return object(map[string]any{
		"kind":        map[string]any{"enum": []string{"route", "instance"}},
		"host":        map[string]any{"type": []string{"string", "null"}},
		"proxy":       map[string]any{"type": []string{"string", "null"}},
		"profile":     stringSchema(),
		"instance_id": stringSchema(),
		"alias":       map[string]any{"type": []string{"string", "null"}},
		"directory":   stringSchema(),
		"service":     map[string]any{"type": []string{"string", "null"}},
		"target_port": map[string]any{"type": []string{"integer", "null"}, "minimum": 1, "maximum": 65535},
		"status":      stringSchema(),
	}, "kind", "host", "proxy", "profile", "instance_id", "alias", "directory", "service", "target_port", "status")
}

func (a *App) proxyStore() (proxy.Manager, *protocol.Error) {
	directory, err := a.dataDirectory()
	if err != nil {
		return proxy.Manager{}, err
	}
	return a.proxyManager(filepath.Dir(directory)), nil
}

func (a *App) registerProxy() {
	for _, action := range []string{"start", "status", "list", "stop"} {
		command := Command{Name: "proxy " + action, Output: proxyStatusSchema()}
		switch action {
		case "start":
			command.Description = "Start or reuse the user-local reverse proxy daemon."
			command.Options = []Option{
				{Name: "port", Pattern: positiveIntegerPattern, Description: "Listener port; defaults to the persistent reservation or 20200."},
				{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe mutation."},
			}
		case "status":
			command.Description = "Show reverse proxy daemon and listener reservation status."
		case "list":
			command.Description = "List routes and instance diagnostics from current project state."
			command.Options = []Option{{Name: "profile", Description: "Filter by profile; omission lists all profiles.", Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128}}
			command.Output = object(map[string]any{"items": map[string]any{"type": "array", "items": proxyItemSchema()}}, "items")
		case "stop":
			command.Description = "Stop the user-local reverse proxy daemon while retaining its port."
			command.Options = []Option{{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe mutation."}}
		}
		command.Run = func(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
			manager, err := a.proxyStore()
			if err != nil {
				return nil, err
			}
			switch action {
			case "status":
				return manager.Status(ctx)
			case "list":
				items, listErr := (proxy.Resolver{Ports: managerPortStore(manager)}).List(ctx, request.Options["profile"])
				return map[string]any{"items": items}, listErr
			case "start":
				var port *int
				if value := request.Options["port"]; value != "" {
					parsed, parseErr := strconv.Atoi(value)
					if parseErr != nil || parsed > 65535 {
						return nil, argumentError("Expected a TCP port from 1 to 65535.", "port")
					}
					port = &parsed
				}
				return manager.Apply(ctx, proxy.Request{Action: action, Port: port, RequestID: request.Options["request-id"]})
			default:
				return manager.Apply(ctx, proxy.Request{Action: action, RequestID: request.Options["request-id"]})
			}
		}
		a.commands = append(a.commands, command)
	}
}

func managerPortStore(manager proxy.Manager) ports.Store {
	return ports.Store{Directory: filepath.Join(manager.Data, "ports")}
}
