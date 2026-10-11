package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (a *App) hostsManager(streams IO) (hosts.Manager, *protocol.Error) {
	d, e := a.currentPaths()
	if e != nil {
		return hosts.Manager{}, portFailure("io_error")
	}
	return hosts.Manager{Data: d.Data, Isolated: d.IsolatedRoot != "", Terminal: interactive(streams), Preview: func(mappings []hosts.Mapping) {
		if interactive(streams) {
			fmt.Fprintln(streams.Err, "Preparing devtools-owned hosts mappings:")
			for _, m := range mappings {
				fmt.Fprintf(streams.Err, "  %s %s\n", m.Address, m.Host)
			}
		}
	}}, nil
}
func (a *App) registerProxyRoutes() {
	a.registerHosts()
	a.registerGlobalRun()
	a.registerExpose()
	for _, action := range []string{"add", "rm", "clear", "up", "down"} {
		c := Command{Name: "proxy " + action, Description: "Manage global declarations or activate/deactivate selected routes without starting backend processes.", Options: []Option{{Name: "global", Boolean: true, Description: "Select projectless global routes."}, {Name: "all", Boolean: true, Description: "Explicitly select all routes in the selected scope."}, {Name: "request-id", Required: true, Pattern: uuidPattern, Description: "Retry-safe request UUID."}}, Output: map[string]any{"type": "object"}}
		if action != "clear" {
			c.Arguments = []Argument{{Name: "name", Required: action == "add" || action == "rm", Pattern: project.ProfilePattern}}
		}
		c.Output = routeMutationSchema(action)
		if action == "add" {
			c.Arguments = append(c.Arguments, Argument{Name: "port", Required: true, Pattern: positiveIntegerPattern})
			c.Options = append(c.Options, Option{Name: "host", Required: true, Description: "DNS hostname."}, Option{Name: "listener", Pattern: project.ProfilePattern, Description: "Listener name; omission selects default."}, Option{Name: "dns", Pattern: "^(auto|hosts|external)$", Description: "DNS preparation policy; omission selects auto."})
		} else if action == "up" || action == "down" {
			c.Options = append(c.Options, Option{Name: "dir", MinLength: 1, Description: "Project directory; omission selects current directory."})
		}
		c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			global, all := r.Options["global"] == "true", r.Options["all"] == "true"
			if (action == "add" || action == "rm" || action == "clear") && !global {
				return nil, argumentError("Project routes are declared in devtools.toml; this command requires --global.", "global")
			}
			if action == "clear" && !all || action != "clear" && all && len(r.Args) > 0 || action == "add" && all || action == "rm" && all {
				return nil, argumentError("Choose a name or explicit --all; clear requires --global --all.", "")
			}
			m, e := a.proxyStore()
			if e != nil {
				return nil, e
			}
			hm, e := a.hostsManager(streams)
			if e != nil {
				return nil, e
			}
			if action == "add" {
				port, e := strconv.Atoi(r.Args[1])
				if e != nil || port < 1 || port > 65535 {
					return nil, argumentError("Expected port 1..65535.", "port")
				}
				return m.AddGlobal(ctx, r.Args[0], port, r.Options["host"], r.Options["listener"], r.Options["dns"], r.Options["request-id"])
			}
			if action == "rm" || action == "clear" {
				return m.RemoveGlobals(ctx, r.Args, all, hm, r.Options["request-id"])
			}
			if !all && len(r.Args) != 1 {
				return nil, argumentError("Choose a route name or --all.", "")
			}
			st, e := managerPortStore(m).Read()
			if e != nil {
				return nil, e
			}
			p := project.Context{}
			refs := []network.RouteRef{}
			if global {
				if all && st.Network != nil {
					for _, route := range st.Network.Routes {
						refs = append(refs, route.Ref())
					}
				} else if !all {
					route := st.GlobalRoute(r.Args[0])
					if route == nil {
						return nil, portFailure("route_not_found")
					}
					refs = append(refs, route.Ref())
				}
			} else {
				dir := r.Options["dir"]
				if dir == "" {
					dir = "."
				}
				p, e = project.Resolve(dir, "")
				if e != nil {
					return nil, e
				}
				instance := st.Find(p.Profile, "", p.Root)
				if instance == nil {
					return nil, portFailure("instance_not_found")
				}
				names := r.Args
				if all {
					names = []string{}
					for name := range p.Proxies {
						names = append(names, name)
					}
					sort.Strings(names)
				}
				for _, name := range names {
					if _, ok := p.Proxies[name]; !ok {
						return nil, portFailure("route_not_found")
					}
					refs = append(refs, network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: name})
				}
			}
			if !all {
				if action == "up" {
					return m.RouteUp(ctx, refs[0], p, os.Environ(), hm, r.Options["request-id"])
				}
				return m.RouteDown(ctx, refs[0], hm, r.Options["request-id"])
			}
			return (netops.Store{Data: m.Data}).Apply(ctx, r.Options["request-id"], struct {
				Action string
				Global bool
				Root   string
			}{action, global, p.Root}, []string{"route-batch"}, func(j *netops.Journal) (any, *protocol.Error) {
				type child struct {
					Ref network.RouteRef
					ID  string
				}
				var selected []child
				present, e := j.Get("selected", &selected)
				if e != nil {
					return nil, e
				}
				if !present {
					for _, ref := range refs {
						selected = append(selected, child{ref, tasks.ID()})
					}
					if e := j.Set("selected", selected); e != nil {
						return nil, e
					}
				}
				items := []any{}
				for _, child := range selected {
					var result any
					var e *protocol.Error
					if action == "up" {
						result, e = m.RouteUp(ctx, child.Ref, p, os.Environ(), hm, child.ID)
					} else {
						result, e = m.RouteDown(ctx, child.Ref, hm, child.ID)
					}
					if e != nil {
						return nil, e
					}
					items = append(items, result)
				}
				return map[string]any{"items": items, "changed": len(items) > 0}, nil
			})
		}
		a.commands = append(a.commands, c)
	}
}
