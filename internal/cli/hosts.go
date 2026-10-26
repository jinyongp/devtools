package cli

import (
	"context"
	"os"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
)

func (a *App) registerHosts() {
	for _, action := range []string{"preview", "apply", "remove"} {
		c := Command{Name: "proxy hosts " + action, Description: "Inspect or repair only devtools-owned hosts mappings; normal route starts prepare these automatically.", Arguments: []Argument{{Name: "name", Required: true, Pattern: project.ProfilePattern}}, Options: []Option{{Name: "global", Boolean: true, Description: "Select a global route."}, {Name: "dir", MinLength: 1, Description: "Project directory."}}, Output: map[string]any{"type": "object"}}
		if action != "preview" {
			c.Options = append(c.Options, Option{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "Retry-safe request UUID."})
		}
		c.Arguments[0].Completion = completeRoute
		if action == "apply" {
			c.Options = append(c.Options, Option{Name: "apply", Required: true, Pattern: "^[0-9a-f]{64}$", Description: "Unchanged hosts digest from preview."})
		}
		if action == "preview" {
			c.Output = object(map[string]any{"digest": stringSchema(), "changed": boolSchema(), "mappings": arraySchema(object(map[string]any{"host": stringSchema(), "address": stringSchema()}, "host", "address"))}, "digest", "changed", "mappings")
		} else {
			c.Output = mutationSchema(map[string]any{"route_ref": routeRefSchema()}, "route_ref")
		}
		c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			m, e := a.proxyStore()
			if e != nil {
				return nil, e
			}
			st, e := managerPortStore(m).Read()
			if e != nil {
				return nil, e
			}
			hm, e := a.hostsManager(streams)
			if e != nil {
				return nil, e
			}
			name := r.Args[0]
			var ref network.RouteRef
			var definition project.Proxy
			if r.Options["global"] == "true" {
				route := st.GlobalRoute(name)
				if route == nil {
					return nil, portFailure("route_not_found")
				}
				ref = route.Ref()
				definition = project.Proxy{Host: route.Host, Listener: route.Listener, DNS: route.DNS}
			} else {
				dir := r.Options["dir"]
				if dir == "" {
					dir = "."
				}
				p, e := project.Resolve(dir, "")
				if e != nil {
					return nil, e
				}
				instance := st.Find(p.Profile, "", p.Root)
				if instance == nil {
					return nil, portFailure("instance_not_found")
				}
				alias := ""
				if instance.Alias != nil {
					alias = *instance.Alias
				}
				resolved, e := p.ResolveNetwork(os.Environ(), alias, name)
				if e != nil {
					return nil, e
				}
				definition = resolved.Context.Proxies[name]
				ref = network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: name}
			}
			listener := st.Listener(definition.Listener)
			if listener == nil {
				return nil, portFailure("listener_not_found")
			}
			dns := definition.DNS
			if dns == "" {
				dns = "auto"
			}
			desired, e := proxy.DNSMapping(definition.Host, dns, *listener)
			if e != nil {
				return nil, e
			}
			owner := ref.Key() + "/" + listener.ID
			if action == "preview" {
				return hm.PreviewChange(owner, desired)
			}
			return (netops.Store{Data: m.Data}).Apply(ctx, r.Options["request-id"], struct {
				Action, Owner, Digest string
				Mappings              any
			}{action, owner, r.Options["apply"], desired}, []string{"hosts-repair"}, func(*netops.Journal) (any, *protocol.Error) {
				if action == "remove" {
					e = hm.RemovePrefix(ctx, ref.Key()+"/")
				} else {
					e = hm.Sync(ctx, owner, desired, r.Options["apply"])
				}
				return map[string]any{"route_ref": ref, "changed": true}, e
			})
		}
		a.commands = append(a.commands, c)
	}
}
