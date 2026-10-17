package cli

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
)

func exposureSchema() map[string]any {
	fields := map[string]any{}
	for _, key := range []string{"id", "provider", "state", "generation", "listener", "host", "url", "reason", "created_at"} {
		fields[key] = stringSchema()
	}
	fields["route_ref"] = routeRefSchema()
	fields["auth"] = map[string]any{"type": "boolean"}
	fields["origin_port"] = map[string]any{"type": "integer"}
	fields["serve_port"] = map[string]any{"type": "integer"}
	fields["client_setup"] = map[string]any{"type": []string{"array", "null"}, "items": stringSchema()}
	return object(fields, "id", "route_ref", "provider", "state", "generation", "listener", "host", "url", "auth", "origin_port", "serve_port", "reason", "created_at")
}
func (a *App) registerExpose() {
	c := Command{Name: "expose start", Description: "Share a selected route through an explicit provider; authentication is read from stdin or a private file.", Arguments: []Argument{{Name: "name", Required: true, Pattern: project.ProfilePattern}}, Options: []Option{{Name: "provider", Default: "local", Pattern: "^(local|lan|cloudflared|tailscale)$", Description: "Sharing provider; local only reads the configured URL."}, {Name: "global", Boolean: true, Description: "Select a projectless global route."}, {Name: "dir", Default: ".", Description: "Project directory."}, {Name: "alias", Description: "LAN .local alias; clients configure DNS themselves."}, {Name: "auth-stdin", Boolean: true, Description: "Read Basic auth user:password from stdin."}, {Name: "auth-file", Description: "Read Basic auth from a private regular file."}, {Name: "no-auth", Boolean: true, Description: "Explicitly allow unauthenticated sharing."}, {Name: "serve-port", Pattern: positiveIntegerPattern, Description: "Tailscale HTTPS port; omission selects 443."}, {Name: "request-id", Pattern: uuidPattern, Description: "Required retry-safe UUID for external sharing."}}, Output: map[string]any{"oneOf": []any{object(map[string]any{"item": exposureSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "item", "changed", "replayed"), object(map[string]any{"item": map[string]any{"type": "object"}, "changed": map[string]any{"type": "boolean"}}, "item", "changed")}}}
	c.Output = map[string]any{"oneOf": []any{mutationSchema(map[string]any{"item": exposureSchema()}, "item"), object(map[string]any{"item": routeEndpointSchema(), "changed": boolSchema()}, "item", "changed")}}
	c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		m, e := a.proxyStore()
		if e != nil {
			return nil, e
		}
		st, e := managerPortStore(m).Read()
		if e != nil {
			return nil, e
		}
		p := project.Context{}
		var ref network.RouteRef
		var endpoint ports.RouteEndpoint
		if r.Options["global"] == "true" {
			route := st.GlobalRoute(r.Args[0])
			if route == nil {
				return nil, portFailure("route_not_found")
			}
			ref = route.Ref()
			endpoint, e = st.GlobalEndpoint(*route)
		} else {
			p, e = project.Resolve(r.Options["dir"], "")
			if e != nil {
				return nil, e
			}
			instance := st.Find(p.Profile, "", p.Root)
			if instance == nil {
				return nil, portFailure("instance_not_found")
			}
			ref = network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: r.Args[0]}
			endpoint, e = st.ProjectEndpoint(p, *instance, r.Args[0])
		}
		if e != nil {
			return nil, e
		}
		if r.Options["provider"] == "local" {
			if r.Options["auth-stdin"] == "true" || r.Options["auth-file"] != "" || r.Options["no-auth"] == "true" || r.Options["alias"] != "" || r.Options["serve-port"] != "" {
				return nil, argumentError("Authentication and provider options require an external provider.", "")
			}
			return map[string]any{"item": endpoint, "changed": false}, nil
		}
		if r.Options["request-id"] == "" {
			return nil, argumentError("External sharing requires --request-id UUID.", "request-id")
		}
		auth, e := readExposureAuth(streams, r)
		if e != nil {
			return nil, e
		}
		hm, e := a.hostsManager(streams)
		if e != nil {
			return nil, e
		}
		servePort := 443
		if value := r.Options["serve-port"]; value != "" {
			servePort, _ = strconv.Atoi(value)
			if servePort < 1 || servePort > 65535 {
				return nil, argumentError("Serve port must be in 1..65535.", "serve-port")
			}
		}
		if r.Options["provider"] != "tailscale" && r.Options["serve-port"] != "" || r.Options["provider"] != "lan" && r.Options["alias"] != "" {
			return nil, argumentError("Option does not apply to the selected provider.", "")
		}
		return m.ExposeStart(ctx, proxy.ExposureRequest{Route: ref, Provider: r.Options["provider"], Alias: r.Options["alias"], Auth: auth, NoAuth: r.Options["no-auth"] == "true", ServePort: servePort, RequestID: r.Options["request-id"]}, p, os.Environ(), hm)
	}
	a.commands = append(a.commands, c)
	for _, action := range []string{"list", "status", "stop"} {
		c := Command{Name: "expose " + action, Description: "Inspect or stop an exposure by its stable execution identity.", Output: object(map[string]any{"item": exposureSchema()}, "item")}
		if action == "list" {
			c.Output = object(map[string]any{"items": map[string]any{"type": "array", "items": exposureSchema()}}, "items")
		} else {
			c.Arguments = []Argument{{Name: "id", Required: true, Pattern: uuidPattern}}
		}
		if action == "stop" {
			c.Options = []Option{{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "Retry-safe request UUID."}}
			c.Output = object(map[string]any{"item": exposureSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "item", "changed", "replayed")
		}
		c.Run = func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			m, e := a.proxyStore()
			if e != nil {
				return nil, e
			}
			switch action {
			case "list":
				items, e := m.ExposureList(ctx)
				return map[string]any{"items": items}, e
			case "status":
				item, e := m.ExposureStatus(ctx, r.Args[0])
				return map[string]any{"item": item}, e
			default:
				return m.ExposeStop(ctx, r.Args[0], r.Options["request-id"])
			}
		}
		a.commands = append(a.commands, c)
	}
}
func readExposureAuth(streams IO, r Request) (string, *protocol.Error) {
	stdin := r.Options["auth-stdin"] == "true"
	path := r.Options["auth-file"]
	if stdin && path != "" || (stdin || path != "") && r.Options["no-auth"] == "true" {
		return "", argumentError("Select one authentication input or --no-auth.", "")
	}
	var reader io.Reader
	if stdin {
		reader = streams.In
	} else if path != "" {
		f, e := platformfs.OpenNoFollow(path, os.O_RDONLY, 0)
		if e != nil {
			return "", portFailure("invalid_authentication_file")
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", portFailure("invalid_authentication_file")
		}
		reader = f
	}
	if reader == nil {
		return "", nil
	}
	data, e := io.ReadAll(io.LimitReader(reader, 8194))
	if e != nil || len(data) > 8193 {
		return "", portFailure("invalid_authentication")
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}
