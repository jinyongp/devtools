package cli

import (
	"context"
	"strconv"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/values"
)

func publicEnvSchema() map[string]any {
	return object(map[string]any{"env": map[string]any{"type": "object", "additionalProperties": stringSchema()}, "env_keys": map[string]any{"type": "array", "items": stringSchema()}, "pending": map[string]any{"type": "array", "items": object(map[string]any{"key": stringSchema(), "code": stringSchema()}, "key", "code")}}, "env", "env_keys", "pending")
}
func (a *App) registerNetworkEnv() {
	c := Command{Name: "command env", Aliases: []string{"cmd env"}, Description: "Read only public network bindings; returns pending diagnostics and excludes profile variables, secrets, and parent environment.", Arguments: []Argument{{Name: "command", Required: true, Pattern: project.CommandPattern}}, Options: []Option{{Name: "dir", Default: ".", Description: "Project directory."}}, Output: publicEnvSchema()}
	c.Run = func(_ context.Context, _ IO, r Request) (any, *protocol.Error) {
		p, e := project.Resolve(r.Options["dir"], "")
		if e != nil {
			return nil, e
		}
		command, ok := p.Commands[r.Args[0]]
		if !ok {
			return nil, portFailure("command_not_found")
		}
		store, e := a.portStore()
		if e != nil {
			return nil, e
		}
		st, e := store.Read()
		if e != nil {
			return nil, e
		}
		env, pending := st.PublicBindings(p, command)
		directory, e := a.dataDirectory()
		if e != nil {
			return nil, e
		}
		secretNames, e := (values.ModernStore{Directory: directory, Profile: p.Profile}).CompletionNames(values.Secret, command.Env)
		if e != nil && e.Code != "env_not_found" {
			return nil, e
		}
		for _, key := range secretNames {
			if _, ok := command.Bind[key]; ok {
				delete(env, key)
				pending = append(pending, ports.PendingBinding{Key: key, Code: "binding_conflict"})
			}
		}
		return map[string]any{"env": env, "env_keys": ports.EnvironmentKeys(env), "pending": pending}, nil
	}
	a.commands = append(a.commands, c)
	c = Command{Name: "proxy env", Description: "Read configured backend and route URLs with separate activation, listener, and upstream diagnostics.", Arguments: []Argument{{Name: "name", Required: true, Pattern: project.ProfilePattern}}, Options: []Option{{Name: "dir", Default: ".", Description: "Project directory."}, {Name: "global", Boolean: true, Description: "Select a global route and its public peer environment."}}, Output: map[string]any{"type": "object"}}
	c.Output = proxyEnvironmentSchema()
	c.Run = func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		m, e := a.proxyStore()
		if e != nil {
			return nil, e
		}
		st, e := managerPortStore(m).Read()
		if e != nil {
			return nil, e
		}
		var endpoint ports.RouteEndpoint
		env := map[string]string{}
		pending := []ports.PendingBinding{}
		if r.Options["global"] == "true" {
			route := st.GlobalRoute(r.Args[0])
			if route == nil {
				return nil, portFailure("route_not_found")
			}
			endpoint, e = st.GlobalEndpoint(*route)
			var envErr *protocol.Error
			env, pending, envErr = st.GlobalEnvironment(r.Args[0])
			if envErr != nil {
				return nil, envErr
			}
		} else {
			p, err := project.Resolve(r.Options["dir"], "")
			if err != nil {
				return nil, err
			}
			instance := st.Find(p.Profile, "", p.Root)
			if instance == nil {
				return map[string]any{"env": env, "env_keys": []string{}, "pending": []ports.PendingBinding{{Key: r.Args[0], Code: "instance_not_found"}}}, nil
			}
			endpoint, e = st.ProjectEndpoint(p, *instance, r.Args[0])
			if e == nil {
				env = map[string]string{"PORT": strconv.Itoa(endpoint.Port), "URL": endpoint.URL, "ROUTE_URL": endpoint.RouteURL}
			}
		}
		if e != nil {
			pending = append(pending, ports.PendingBinding{Key: r.Args[0], Code: e.Code})
		}
		items, listErr := proxy.NewResolver(managerPortStore(m)).List(ctx, "")
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range items {
			if item.Ref() == endpoint.Ref {
				endpoint.Status = item.Status
			}
		}
		listener := any(nil)
		enabled, err := m.NetworkEnabled()
		if err != nil {
			return nil, err
		}
		if enabled {
			listener, err = m.NetworkStatus(ctx, endpoint.Listener)
		} else {
			var status proxy.Status
			status, err = m.Status(ctx)
			listener = proxyStatusResult(status)
		}
		if err != nil {
			pending = append(pending, ports.PendingBinding{Key: "listener", Code: err.Code})
		}
		result := map[string]any{"env": env, "env_keys": ports.EnvironmentKeys(env), "pending": pending, "listener": listener, "upstream_ready": endpoint.Port > 0 && ports.Reachable(ctx, endpoint.Port)}
		if endpoint.Ref.Valid() {
			result["item"] = endpoint
		}
		return result, nil
	}
	a.commands = append(a.commands, c)
}
