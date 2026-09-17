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
		command := Command{Name: "proxy " + action, Output: itemOutput(proxyStatusSchema())}
		if action == "start" || action == "stop" {
			command.Output = object(map[string]any{"item": proxyStatusSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "item", "changed", "replayed")
		}
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
			q := proxy.Request{Action: action, RequestID: request.Options["request-id"]}
			switch action {
			case "status":
				item, err := manager.Status(ctx)
				return map[string]any{"item": proxyStatusResult(item)}, err
			case "list":
				items, listErr := (proxy.Resolver{Ports: managerPortStore(manager)}).List(ctx, request.Options["profile"])
				return map[string]any{"items": items}, listErr
			case "start":
				if value := request.Options["port"]; value != "" {
					parsed, parseErr := strconv.Atoi(value)
					if parseErr != nil || parsed > 65535 {
						return nil, argumentError("Expected a TCP port from 1 to 65535.", "port")
					}
					q.Port = &parsed
				}
			}
			result, err := manager.Apply(ctx, q)
			return map[string]any{"item": proxyStatusResult(result), "changed": result.Changed, "replayed": result.Replayed}, err
		}
		a.commands = append(a.commands, command)
	}
}

// Mutation metadata belongs beside the resource, including false values.
func proxyStatusResult(status proxy.Status) map[string]any {
	return map[string]any{"running": status.Running, "state": status.State, "port": status.Port, "url": status.URL, "started_at": status.StartedAt, "reason": status.Reason}
}

func managerPortStore(manager proxy.Manager) ports.Store {
	return ports.Store{Directory: filepath.Join(manager.Data, "ports")}
}
