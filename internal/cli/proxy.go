package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/network"
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
		"id":         stringSchema(), "name": stringSchema(), "generation": stringSchema(), "https_port": map[string]any{"type": "integer"}, "daemon_running": map[string]any{"type": "boolean"},
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
		"listener":    stringSchema(),
		"host_policy": hostPolicySchema(),
		"scope":       stringSchema(), "route_id": stringSchema(),
	}, "kind", "host", "proxy", "profile", "instance_id", "alias", "directory", "service", "target_port", "status")
}

func (a *App) proxyStore() (proxy.Manager, *protocol.Error) {
	directory, err := a.dataDirectory()
	if err != nil {
		return proxy.Manager{}, err
	}
	m := a.proxyManager(filepath.Dir(directory))
	directories, e := a.currentPaths()
	if e != nil {
		return m, portFailure("io_error")
	}
	m.IsolatedRoot = directories.IsolatedRoot
	return m, nil
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
			command.Options = append(command.Options, Option{Name: "global", Boolean: true, Description: "List only global routes; excludes --profile."})
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
			if action != "list" {
				enabled, e := manager.NetworkEnabled()
				if e != nil {
					return nil, e
				}
				name, all := request.Options["listener"], request.Options["all"] == "true"
				if all && name != "" || all && request.Options["port"] != "" {
					return nil, argumentError("--all excludes --listener and --port.", "")
				}
				if enabled || name != "" && name != network.DefaultListener || all && action == "start" {
					if action == "status" {
						if all {
							items, e := manager.NetworkStatuses(ctx)
							return map[string]any{"items": items}, e
						}
						item, e := manager.NetworkStatus(ctx, name)
						return map[string]any{"item": item}, e
					}
					port := 0
					if value := request.Options["port"]; value != "" {
						parsed, e := strconv.Atoi(value)
						if e != nil || parsed < 1 || parsed > 65535 {
							return nil, argumentError("Expected TCP port 1..65535.", "port")
						}
						port = parsed
					}
					return manager.NetworkApply(ctx, action, name, all, request.Options["request-id"], port)
				}
			}
			switch action {
			case "status":
				item, err := manager.Status(ctx)
				if request.Options["all"] == "true" {
					return map[string]any{"items": []any{proxyStatusResult(item)}}, err
				}
				return map[string]any{"item": proxyStatusResult(item)}, err
			case "list":
				if request.Options["global"] == "true" && request.Options["profile"] != "" {
					return nil, argumentError("Choose --global or --profile.", "")
				}
				items, listErr := (proxy.Resolver{Ports: managerPortStore(manager)}).List(ctx, request.Options["profile"])
				if request.Options["global"] == "true" {
					filtered := []proxy.Item{}
					for _, item := range items {
						if item.Scope == "global" {
							filtered = append(filtered, item)
						}
					}
					items = filtered
				}
				return map[string]any{"items": publicHostPolicy(items)}, listErr
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
			if request.Options["all"] == "true" {
				return map[string]any{"items": []any{proxyStatusResult(result)}, "changed": result.Changed, "replayed": result.Replayed}, err
			}
			return map[string]any{"item": proxyStatusResult(result), "changed": result.Changed, "replayed": result.Replayed}, err
		}
		if action != "list" {
			command.Options = append(command.Options, Option{Name: "listener", Pattern: project.ProfilePattern, Description: "Named listener; omission selects default."}, Option{Name: "all", Boolean: true, Description: "Select all configured listeners."})
			command.Output = listenerOutput(action != "status")
		}
		a.commands = append(a.commands, command)
	}
	a.registerNetworkProxy()
	a.registerProxyRoutes()
}

// Mutation metadata belongs beside the resource, including false values.
func proxyStatusResult(status proxy.Status) map[string]any {
	return map[string]any{"running": status.Running, "state": status.State, "port": status.Port, "url": status.URL, "started_at": status.StartedAt, "reason": status.Reason}
}

func managerPortStore(manager proxy.Manager) ports.Store {
	return ports.Store{Directory: filepath.Join(manager.Data, "ports")}
}
