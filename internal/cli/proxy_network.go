package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) registerNetworkProxy() {
	a.registerNetsetup()
	a.registerNetworkTeardown()
	c := Command{Name: "proxy configure", Description: "Configure a stopped named HTTP/HTTPS listener. HTTPS is opt-in.", Options: []Option{{Name: "listener", Pattern: project.ProfilePattern, Description: "Listener name; omission selects default."}, {Name: "http-address", MinLength: 1, Description: "HTTP IP:port."}, {Name: "https-address", MinLength: 1, Description: "Enable HTTPS at this IP:port."}, {Name: "disable-https", Boolean: true, Description: "Disable HTTPS on a stopped listener."}, {Name: "request-id", Required: true, Pattern: uuidPattern, Description: "Retry-safe request UUID."}}, Output: map[string]any{"type": "object"}}
	c.Run = func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		m, e := a.proxyStore()
		if e != nil {
			return nil, e
		}
		return m.Configure(ctx, r.Options["listener"], r.Options["http-address"], r.Options["https-address"], r.Options["disable-https"] == "true", r.Options["request-id"])
	}
	c.Output = mutationSchema(map[string]any{"item": listenerDefinitionSchema()}, "item")
	a.commands = append(a.commands, c)
	for _, action := range []string{"restart", "logs"} {
		c := Command{Name: "proxy " + action, Description: "Operate the selected listener; omission selects default.", Options: []Option{{Name: "listener", Pattern: project.ProfilePattern, Description: "Registered listener name."}, {Name: "all", Boolean: true, Description: "Select all configured listeners."}}, Output: map[string]any{"type": "object"}}
		if action == "restart" {
			c.Options = append(c.Options, Option{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "Retry-safe request UUID."})
			c.Output = listenerOutput(true)
		} else {
			f := map[string]any{}
			for _, key := range []string{"time", "listener", "generation", "state", "reason"} {
				f[key] = stringSchema()
			}
			c.Output = object(map[string]any{"items": arraySchema(object(f, "time", "listener", "generation", "state", "reason"))}, "items")
		}
		c.Run = func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			m, e := a.proxyStore()
			if e != nil {
				return nil, e
			}
			if r.Options["all"] == "true" && r.Options["listener"] != "" {
				return nil, argumentError("--all excludes --listener.", "")
			}
			if action == "logs" {
				items, e := m.NetworkLogs(r.Options["listener"], r.Options["all"] == "true")
				return map[string]any{"items": items}, e
			}
			return m.NetworkApply(ctx, "restart", r.Options["listener"], r.Options["all"] == "true", r.Options["request-id"])
		}
		a.commands = append(a.commands, c)
	}
}

func listenerOutput(mutation bool) map[string]any {
	single := map[string]any{"item": proxyStatusSchema()}
	multiple := map[string]any{"items": map[string]any{"type": "array", "items": proxyStatusSchema()}}
	singleRequired := []string{"item"}
	multipleRequired := []string{"items"}
	if mutation {
		for _, p := range []map[string]any{single, multiple} {
			p["changed"] = map[string]any{"type": "boolean"}
			p["replayed"] = map[string]any{"type": "boolean"}
		}
		singleRequired = append(singleRequired, "changed", "replayed")
		multipleRequired = append(multipleRequired, "changed", "replayed")
	}
	return map[string]any{"oneOf": []any{object(single, singleRequired...), object(multiple, multipleRequired...)}}
}
