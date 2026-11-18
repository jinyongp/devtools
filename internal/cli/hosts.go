package cli

import (
	"context"
	"os"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
)

func (a *App) registerHosts() {
	a.commands = append(a.commands,
		Command{Name: "proxy hosts status", Description: "Inspect the pending hosts operation in the selected data store.", Output: object(map[string]any{
			"item": hostsRecoveryStatusSchema(), "next_actions": arraySchema(remedySchema()),
		}, "item", "next_actions"), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			hm, e := a.hostsManager(ctx, streams)
			if e != nil {
				return nil, e
			}
			item := hm.RecoveryStatus()
			actions := []protocol.Remedy{}
			if item.Digest != "" {
				id := item.RecoveryRequestID
				if id == "" {
					id = protocol.NewRequestID()
				}
				effect, message := "system_write", "Apply the reviewed pending mappings and record the hosts step."
				if item.RecoveryAction == "finalize" {
					effect, message = "value_write", "Record the hosts step using the verified file."
				}
				argv := []string{"devtools", "proxy", "hosts", "recover", "--apply", item.Digest, "--request-id", id}
				actions = append(actions, protocol.LocalAction(argv, nil, message, effect).Remedy(r.Invocation))
			}
			return map[string]any{"item": item, "next_actions": actions}, nil
		}},
		Command{Name: "proxy hosts recover", Description: "Resolve one confirmed hosts step using the token from hosts status.", Options: []Option{hostsConfirmationOption(), requestIDOption(uuidPattern)}, Output: mutationSchema(map[string]any{
			"item": object(map[string]any{
				"state": enumStrings("resolved"), "method": enumStrings("apply_saved", "finalize", "rebase"), "os_write": hostsWriteSchema(),
				"origin_request_id": stringSchema(), "step_id": stringSchema(),
			}, "state", "method", "os_write"),
			"request_id": stringSchema(),
		}, "item", "request_id"), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			hm, e := a.hostsManager(ctx, streams)
			if e != nil {
				return nil, e
			}
			return hm.Recover(ctx, r.Options["request-id"], r.Options["apply"])
		}},
	)
	for _, action := range []string{"preview", "apply", "remove"} {
		c := Command{Name: "proxy hosts " + action, Description: "Inspect or repair only devtools-owned hosts mappings; normal route starts prepare these automatically.", Arguments: []Argument{{Name: "name", Required: true, Pattern: project.ProfilePattern}}, Options: []Option{{Name: "global", Boolean: true, Description: "Select a global route."}, {Name: "dir", MinLength: 1, Description: "Project directory."}}, Output: map[string]any{"type": "object"}}
		if action != "preview" {
			c.Options = append(c.Options, requestIDOption(uuidPattern))
		}
		c.Arguments[0].Completion = completeRoute
		if action == "apply" {
			c.Options = append(c.Options, hostsConfirmationOption())
		}
		if action == "preview" {
			c.Output = object(map[string]any{"digest": stringSchema(), "changed": boolSchema(), "mappings": arraySchema(object(map[string]any{"host": stringSchema(), "address": stringSchema()}, "host", "address"))}, "digest", "changed", "mappings")
			c.Output["properties"].(map[string]any)["next_actions"] = arraySchema(remedySchema())
		} else {
			c.Output = mutationSchema(map[string]any{"route_ref": routeRefSchema(), "os_write": hostsWriteSchema()}, "route_ref")
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
			hm, e := a.hostsManager(ctx, streams)
			if e != nil {
				return nil, e
			}
			name := r.Args[0]
			var ref network.RouteRef
			var definition project.Proxy
			directory := ""
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
				p, e := a.resolveStoredProject(dir, "")
				if e != nil {
					return nil, e
				}
				directory = p.Root
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
				result, e := hm.PreviewChange(owner, desired)
				if e != nil {
					return nil, e
				}
				argv := []string{"devtools", "proxy", "hosts", "apply", name, "--apply", result["digest"].(string), "--request-id", protocol.NewRequestID()}
				if r.Options["global"] == "true" {
					argv = append(argv, "--global")
				} else {
					argv = append(argv, "--dir", directory)
				}
				actions := []protocol.Remedy{}
				if effect, _ := result["effect"].(string); effect != "" {
					actions = append(actions, protocol.LocalAction(argv, nil, "Apply the reviewed hosts mappings.", effect).Remedy(r.Invocation))
				}
				delete(result, "effect")
				result["next_actions"] = actions
				return result, nil
			}
			return (netops.Store{Data: m.Data}).Apply(ctx, r.Options["request-id"], struct {
				Action, Owner, Digest string
				Mappings              any
			}{action, owner, r.Options["apply"], desired}, []string{"hosts-repair"}, func(*netops.Journal) (any, *protocol.Error) {
				hm := hm.ForRequest(r.Options["request-id"], "hosts-"+action, ref, listener.ID)
				var result map[string]any
				if action == "remove" {
					result, e = hm.RemovePrefixResult(ctx, ref.Key()+"/")
				} else {
					result, e = hm.Change(ctx, owner, desired, r.Options["apply"])
				}
				if e != nil {
					if action == "apply" && e.Code == "stale_preview" {
						argv := []string{"devtools", "proxy", "hosts", "preview", name}
						if directory != "" {
							argv = append(argv, "--dir", directory)
						} else {
							argv = append(argv, "--global")
						}
						e.Details = map[string]any{
							"reason": "hosts_confirmation_changed", "retry": "after_refresh",
							"remedies": []protocol.Remedy{protocol.LocalAction(argv, nil, "Review the current mappings and apply with a new request UUID.", "read").Remedy(r.Invocation)},
						}
					}
					return nil, e
				}
				return map[string]any{"route_ref": ref, "changed": result["changed"], "os_write": result["os_write"]}, nil
			})
		}
		a.commands = append(a.commands, c)
	}
}

func hostsConfirmationOption() Option {
	return Option{Name: "apply", Required: true, Pattern: "^[0-9a-f]{64}$", Description: "Use the confirmation digest printed by the corresponding hosts read command."}
}

func enumStrings(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func hostsWriteSchema() map[string]any {
	return enumStrings("performed", "not_performed", "unknown")
}

func hostsRecoveryFields() map[string]any {
	return map[string]any{
		"state": enumStrings("none", "before", "after", "diverged", "unavailable"), "pending": map[string]any{"type": []string{"boolean", "null"}},
		"recovery_action": enumStrings("none", "apply_saved", "finalize", "rebase", "blocked"),
		"reason":          enumStrings("references_unavailable", "hosts_unavailable", "ownership_unknown", "mapping_conflict", "divergence_ambiguous", "isolated_write", "helper_input_too_large"),
	}
}

func hostsRecoveryStatusSchema() map[string]any {
	f := hostsRecoveryFields()
	f["changes"] = arraySchema(object(map[string]any{
		"action": enumStrings("add", "remove"), "owner": stringSchema(), "host": stringSchema(), "address": stringSchema(),
	}, "action", "owner", "host", "address"))
	for _, key := range []string{"origin_request_id", "step_id", "listener_id", "recovery_request_id", "digest"} {
		f[key] = stringSchema()
	}
	f["route_ref"] = routeRefSchema()
	return object(f, "state", "pending", "changes", "recovery_action")
}

func routeHostsAction(issue, name, directory, scope string, invocation protocol.InvocationContext) (protocol.Remedy, bool) {
	switch issue {
	case "hosts_recovery_required":
		return hosts.StatusAction().Remedy(invocation), true
	case "hosts_mapping_missing":
		if name == "" || scope != "global" && (scope != "project" || directory == "") {
			break
		}
		argv := []string{"devtools", "proxy", "hosts", "preview", name}
		if scope == "global" {
			argv = append(argv, "--global")
		} else {
			argv = append(argv, "--dir", directory)
		}
		return protocol.LocalAction(argv, nil, "Review the missing mapping for this route.", "read").Remedy(invocation), true
	}
	return protocol.Remedy{}, false
}
