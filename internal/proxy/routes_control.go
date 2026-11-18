package proxy

import (
	"context"
	"net/netip"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (m Manager) AddGlobal(ctx context.Context, name string, port int, host, listener, dns, id string) (any, *protocol.Error) {
	return m.AddGlobalWithHostPolicy(ctx, name, port, host, listener, dns, "", id)
}

func (m Manager) AddGlobalWithHostPolicy(ctx context.Context, name string, port int, host, listener, dns string, policy network.HostPolicy, id string) (any, *protocol.Error) {
	if !policy.Valid() {
		return nil, proxyFailure("invalid_argument")
	}
	policy = policy.Canonical()
	listener = network.ListenerName(listener)
	if dns == "" {
		dns = "auto"
	}
	host = strings.ToLower(host)
	if !project.ValidProfile(name) || !network.ValidHost(host) || port < 1 || port > 65535 {
		return nil, proxyFailure("invalid_argument")
	}
	input := struct {
		Action, Name, Host, Listener, DNS string
		Port                              int
		HostPolicy                        network.HostPolicy `json:",omitempty"`
	}{"add", name, host, listener, dns, port, policy}
	return (netops.Store{Data: m.Data}).Apply(ctx, id, input, []string{"global-routes"}, func(j *netops.Journal) (any, *protocol.Error) {
		var routeID string
		present, e := j.Get("route_id", &routeID)
		if e != nil {
			return nil, e
		}
		if !present {
			routeID = tasks.ID()
			if e := j.Set("route_id", routeID); e != nil {
				return nil, e
			}
		}
		route := network.GlobalRoute{ID: routeID, Name: name, Host: host, Reservation: ports.GlobalReservation(routeID), Listener: listener, DNS: dns, HostPolicy: policy}
		if !route.Valid() {
			return nil, proxyFailure("invalid_argument")
		}
		e = m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			if old := st.GlobalRoute(name); old != nil {
				if *old == route && st.Reservation(old.Reservation) != nil && st.Reservation(old.Reservation).Port == port {
					return false, nil
				}
				return false, proxyFailure("route_exists")
			}
			st.UpgradeNetwork()
			if _, e := st.Reference(route.Reservation, port); e != nil {
				return false, e
			}
			st.Network.Routes = append(st.Network.Routes, route)
			return true, st.SetActivation(network.Activation{Route: route.Ref(), Mode: "disabled"})
		})
		return map[string]any{"item": route, "changed": true}, e
	})
}

type routeCandidate struct {
	Activation   network.Activation `json:"activation"`
	Listener     network.Listener   `json:"listener"`
	Port         int                `json:"port"`
	Service      string             `json:"service"`
	StartID      string             `json:"start_id"`
	HostsAttempt string             `json:"hosts_attempt,omitempty"`
	HostPolicy   network.HostPolicy `json:"host_policy,omitempty"`
}

func publicActivation(a network.Activation) map[string]any {
	return map[string]any{"route_ref": a.Route, "active": a.Mode == "active", "host": a.Host, "listener": a.Listener, "dns": a.DNS}
}

func DNSMapping(host, dns string, listener network.Listener) ([]hosts.Mapping, *protocol.Error) {
	if dns == "external" || dns == "auto" && (host == "localhost" || strings.HasSuffix(host, ".localhost")) {
		return nil, nil
	}
	address := listener.HTTPHost
	if listener.HTTPSReservation != "" {
		address = listener.HTTPSHost
	}
	ip, e := netip.ParseAddr(address)
	if e == nil && ip.IsUnspecified() {
		if ip.Is4() {
			ip = netip.MustParseAddr("127.0.0.1")
		} else {
			ip = netip.IPv6Loopback()
		}
	}
	if e != nil || !ip.IsLoopback() {
		return nil, protocol.NewError("dns_address_required", "Automatic hosts preparation requires a loopback or wildcard listener address; use external DNS for a specific remote address.", 3, nil)
	}
	return []hosts.Mapping{{Host: host, Address: ip.String()}}, nil
}
func routeNetworkDigest(ref network.RouteRef, host, dns string, listener network.Listener, httpPort, httpsPort, backend int, policy network.HostPolicy) string {
	return project.Digest(struct {
		Ref                  network.RouteRef
		Host, DNS            string
		Listener             network.Listener
		HTTP, HTTPS, Backend int
		HostPolicy           network.HostPolicy `json:",omitempty"`
	}{ref, host, dns, listener, httpPort, httpsPort, backend, policy.Canonical()})
}

func BuildProjectActivation(st *ports.State, ref network.RouteRef, definition project.Proxy, host, source string, backed bool) (network.Activation, *protocol.Error) {
	listenerName := network.ListenerName(definition.Listener)
	listener := st.Listener(listenerName)
	if listener == nil {
		return network.Activation{}, proxyFailure("listener_not_found")
	}
	assignment := st.Get(ref.InstanceID, definition.Port)
	if assignment == nil {
		return network.Activation{}, proxyFailure("assignment_missing")
	}
	dns := definition.DNS
	if dns == "" {
		dns = "auto"
	}
	h, t, e := st.ListenerPorts(listenerName)
	if e != nil {
		return network.Activation{}, e
	}
	a := network.Activation{Route: ref, Mode: "active", Host: host, Listener: listenerName, DNS: dns, ConfigDigest: project.Digest(definition), SourceDigest: source, EnvironmentBacked: backed, NetworkDigest: routeNetworkDigest(ref, host, dns, *listener, h, t, assignment.Port, definition.HostPolicy)}
	if !a.Valid() {
		return a, proxyFailure("host_invalid")
	}
	return a, nil
}
func (m Manager) RouteUp(ctx context.Context, ref network.RouteRef, p project.Context, parent []string, hm hosts.Manager, id string) (any, *protocol.Error) {
	if !ref.Valid() {
		return nil, proxyFailure("invalid_argument")
	}
	input := struct {
		Action string
		Route  network.RouteRef
	}{"up", ref}
	return (netops.Store{Data: m.Data}).Apply(ctx, id, input, []string{"route/" + ref.Key()}, func(j *netops.Journal) (any, *protocol.Error) {
		var candidate routeCandidate
		present, e := j.Get("candidate", &candidate)
		if e != nil {
			return nil, e
		}
		st, e := m.portStore().Read()
		if e != nil {
			return nil, e
		}
		if st.Pinned(ref) {
			return nil, st.RoutePinConflict(ref)
		}
		if !present {
			var host, listener, dns, configDigest, source string
			var policy network.HostPolicy
			backed := false
			port := 0
			service := ""
			if ref.Scope == "global" {
				var route *network.GlobalRoute
				if st.Network != nil {
					for i := range st.Network.Routes {
						if st.Network.Routes[i].ID == ref.RouteID {
							route = &st.Network.Routes[i]
						}
					}
				}
				if route == nil {
					return nil, proxyFailure("route_not_found")
				}
				host, listener, dns = route.Host, route.Listener, route.DNS
				policy = route.HostPolicy.Canonical()
				configDigest = project.Digest(route)
				source = configDigest
				if reservation := st.Reservation(route.Reservation); reservation != nil {
					port = reservation.Port
				}
			} else {
				instance := st.ByID(ref.InstanceID)
				if instance == nil || instance.Profile != p.Profile || instance.Directory != p.Root {
					return nil, proxyFailure("instance_conflict")
				}
				alias := ""
				if instance.Alias != nil {
					alias = *instance.Alias
				}
				resolved, e := p.ResolveNetwork(parent, alias, ref.Proxy)
				if e != nil {
					return nil, e
				}
				definition := resolved.Context.Proxies[ref.Proxy]
				host, listener, dns = definition.Host, network.ListenerName(definition.Listener), definition.DNS
				policy = definition.HostPolicy.Canonical()
				service = definition.Port
				source = resolved.SourceDigest
				configDigest = project.Digest(p.Proxies[ref.Proxy])
				backed = resolved.EnvironmentBacked[ref.Proxy]
				if a := st.Get(ref.InstanceID, service); a != nil {
					port = a.Port
				}
			}
			if !network.ValidHost(host) {
				return nil, proxyFailure("host_invalid")
			}
			if port == 0 {
				return nil, proxyFailure("assignment_missing")
			}
			if dns == "" {
				dns = "auto"
			}
			if st.Listener(listener) == nil && listener == network.DefaultListener {
				var configureID string
				found, e := j.Get("configure_id", &configureID)
				if e != nil {
					return nil, e
				}
				if !found {
					configureID = tasks.ID()
					if e := j.Set("configure_id", configureID); e != nil {
						return nil, e
					}
				}
				if _, e := m.Configure(ctx, listener, "", "", false, configureID); e != nil {
					return nil, e
				}
				st, e = m.portStore().Read()
				if e != nil {
					return nil, e
				}
			}
			definition := st.Listener(listener)
			if definition == nil {
				return nil, proxyFailure("listener_not_found")
			}
			httpPort, httpsPort, e := st.ListenerPorts(listener)
			if e != nil {
				return nil, e
			}
			candidate = routeCandidate{Activation: network.Activation{Route: ref, Mode: "active", Host: host, Listener: listener, DNS: dns, ConfigDigest: configDigest, SourceDigest: source, NetworkDigest: routeNetworkDigest(ref, host, dns, *definition, httpPort, httpsPort, port, policy), EnvironmentBacked: backed}, Listener: *definition, Port: port, Service: service, StartID: tasks.ID(), HostPolicy: policy}
			if candidate.Listener.HTTPSReservation != "" {
				if e := j.ReferenceTLS(candidate.Listener.ID); e != nil {
					return nil, e
				}
			}
			if e := j.Set("candidate", candidate); e != nil {
				return nil, e
			}
		}
		a := candidate.Activation
		owner := ref.Key() + "/" + candidate.Listener.ID
		cleanup := func() (bool, *protocol.Error) {
			changed := false
			desired, e := DNSMapping(a.Host, a.DNS, candidate.Listener)
			if e != nil {
				return false, e
			}
			if len(desired) == 0 {
				old, e := hm.OwnerMappings(owner)
				if e != nil {
					return false, e
				}
				if len(old) > 0 {
					result, e := hm.ForRequest(id, "up-cleanup", ref, candidate.Listener.ID).Change(ctx, owner, nil, "")
					if e != nil {
						return false, e
					}
					changed = result["changed"] == true
				}
			}
			result, e := hm.ForRequest(id, "up-cleanup", ref, candidate.Listener.ID).RemoveOtherOwnersResult(ctx, ref.Key()+"/", owner)
			return changed || result["changed"] == true, e
		}
		if e := m.validateCandidate(st, candidate, p); e != nil {
			return nil, e
		}
		// Reject collisions before preparing DNS or starting a listener.
		m.hostSystem = hm.System
		items, e := m.resolver().List(ctx, "")
		if e != nil {
			return nil, e
		}
		for _, item := range items {
			if item.Host != nil && *item.Host == a.Host && item.Ref() != ref {
				return nil, proxyFailure("host_conflict")
			}
		}
		mapping, e := DNSMapping(a.Host, a.DNS, candidate.Listener)
		if e != nil {
			return nil, e
		}
		var previous []hosts.Mapping
		previousSaved, e := j.Get("previous_hosts", &previous)
		if e != nil {
			return nil, e
		}
		if !previousSaved {
			previous, e = hm.OwnerMappings(owner)
			if e != nil {
				return nil, e
			}
			if e := j.Set("previous_hosts", previous); e != nil {
				return nil, e
			}
		}
		if candidate.HostsAttempt == "" {
			candidate.HostsAttempt = candidate.StartID
			if e := j.Set("candidate", candidate); e != nil {
				return nil, e
			}
		}
		type compensation struct {
			Attempt string
			Proof   *hosts.Publication
			Cause   receiptError
		}
		rollback := func(plan compensation) *protocol.Error {
			result, e := hm.ForRequest(id, "up-rollback-"+plan.Attempt, ref, candidate.Listener.ID).IfPublished(plan.Proof).Change(ctx, owner, previous, "")
			if e != nil {
				return activationRollbackFailure(plan.Cause, e)
			}
			if result["changed"] == true || result["replayed"] == true {
				candidate.StartID, candidate.HostsAttempt = tasks.ID(), tasks.ID()
				if e := j.Set("candidate", candidate); e != nil {
					return e
				}
			}
			return j.Set("compensation", nil)
		}
		var saved *compensation
		if _, e := j.Get("compensation", &saved); e != nil {
			return nil, e
		}
		if saved != nil {
			if e := rollback(*saved); e != nil {
				return nil, e
			}
		}
		hostChanged := false
		var published *hosts.Publication
		if len(mapping) > 0 {
			result, e := hm.ForRequest(id, "up-"+candidate.HostsAttempt, ref, candidate.Listener.ID).Change(ctx, owner, mapping, "")
			if e != nil {
				return nil, e
			}
			hostChanged, published = result["changed"] == true, hosts.Published(result)
			if issue := hm.Observe().MappingIssue(owner, mapping[0]); issue != "" {
				return nil, routeDNSFailure(issue)
			}
		}
		fail := func(original *protocol.Error) *protocol.Error {
			if !hostChanged || original.Code == "proxy_pending" || original.Code == "proxy_unavailable" || original.Code == "canceled" {
				if hostChanged {
					return protocol.WithOutcomes(original, "partial", routeOutcome(ref, "hosts", "committed", true))
				}
				return original
			}
			plan := compensation{candidate.HostsAttempt, published, receiptError{original.Code, original.Message, original.Details, original.ExitCode}}
			if published == nil {
				return activationRollbackFailure(plan.Cause, routeDNSFailure("hosts_unavailable"))
			}
			if e := j.Set("compensation", plan); e != nil {
				return activationRollbackFailure(plan.Cause, e)
			}
			if e := rollback(plan); e != nil {
				return e
			}
			return protocol.WithOutcomes(original, "none", routeOutcome(ref, "hosts", "rolled_back", true))
		}
		listenerChanged, e := m.prepareRouteListener(ctx, a.Listener, &candidate.StartID, func() *protocol.Error {
			return j.Set("candidate", candidate)
		})
		if e != nil {
			return nil, fail(e)
		}
		intentChanged := false
		e = m.portStore().Update(ctx, func(current *ports.State) (bool, *protocol.Error) {
			if e := m.validateCandidate(current, candidate, p); e != nil {
				return false, e
			}
			if existing := current.Activation(ref); existing != nil && *existing == a {
				return false, nil
			}
			intentChanged = true
			return true, current.SetActivation(a)
		})
		if e != nil {
			return nil, fail(e)
		}
		cleaned, e := cleanup()
		if e == nil {
			e = m.VerifyRoutePrepared(ctx, ref, hm)
		}
		changed := hostChanged || listenerChanged || intentChanged || cleaned
		if e != nil {
			if changed {
				e = protocol.WithOutcomes(e, "partial", routeOutcome(ref, "route", "committed", true))
			}
			return nil, e
		}
		item := publicActivation(a)
		item["prepared"] = true
		return map[string]any{"item": item, "changed": changed}, nil
	})
}
func (m Manager) validateCandidate(st *ports.State, c routeCandidate, p project.Context) *protocol.Error {
	a := c.Activation
	listener := st.Listener(a.Listener)
	if listener == nil || *listener != c.Listener {
		return proxyFailure("request_conflict")
	}
	h, t, e := st.ListenerPorts(a.Listener)
	if e != nil {
		return e
	}
	port := 0
	if a.Route.Scope == "project" {
		instance := st.ByID(a.Route.InstanceID)
		if instance == nil || instance.Directory != p.Root || instance.Profile != p.Profile {
			return proxyFailure("instance_conflict")
		}
		if assignment := st.Get(instance.ID, c.Service); assignment != nil {
			port = assignment.Port
		}
		source, e := p.NetworkSources()
		if e != nil {
			return e
		}
		if source != a.SourceDigest {
			return proxyFailure("activation_required")
		}
	} else {
		if st.Network != nil {
			for _, r := range st.Network.Routes {
				if r.Ref() == a.Route && project.Digest(r) == a.ConfigDigest {
					if reservation := st.Reservation(r.Reservation); reservation != nil {
						port = reservation.Port
					}
				}
			}
		}
	}
	if port != c.Port || routeNetworkDigest(a.Route, a.Host, a.DNS, *listener, h, t, port, c.HostPolicy) != a.NetworkDigest {
		return proxyFailure("request_conflict")
	}
	return nil
}
func (m Manager) RouteDown(ctx context.Context, ref network.RouteRef, hm hosts.Manager, id string) (any, *protocol.Error) {
	if !ref.Valid() {
		return nil, proxyFailure("invalid_argument")
	}
	return (netops.Store{Data: m.Data}).Apply(ctx, id, struct {
		Action string
		Route  network.RouteRef
	}{"down", ref}, []string{"route/" + ref.Key()}, func(j *netops.Journal) (any, *protocol.Error) {
		proof, e := m.disableRoute(ctx, ref, j, nil)
		if e != nil {
			return nil, e
		}
		cleaned, e := m.cleanupDisabledRoute(ctx, ref, proof, hm.ForRequest(id, "down", ref, ""))
		if e != nil {
			return nil, cleanupFailure(e, ref, proof.Changed)
		}
		return map[string]any{"route_ref": ref, "changed": proof.Changed || cleaned}, nil
	})
}
func (m Manager) RemoveGlobals(ctx context.Context, names []string, all bool, hm hosts.Manager, id string) (any, *protocol.Error) {
	if all && len(names) > 0 || !all && len(names) == 0 {
		return nil, proxyFailure("invalid_argument")
	}
	sort.Strings(names)
	return (netops.Store{Data: m.Data}).Apply(ctx, id, struct {
		Action string
		Names  []string
		All    bool
	}{"remove", names, all}, []string{"global-routes"}, func(j *netops.Journal) (any, *protocol.Error) {
		var targets []network.GlobalRoute
		present, e := j.Get("targets", &targets)
		if e != nil {
			return nil, e
		}
		st, e := m.portStore().Read()
		if e != nil {
			return nil, e
		}
		if !present {
			if all && st.Network != nil {
				targets = append(targets, st.Network.Routes...)
			} else {
				for _, name := range names {
					r := st.GlobalRoute(name)
					if r == nil {
						return nil, proxyFailure("route_not_found")
					}
					targets = append(targets, *r)
				}
			}
			if e := j.Set("targets", targets); e != nil {
				return nil, e
			}
		}
		for _, target := range targets {
			if st.Pinned(target.Ref()) {
				return nil, st.RoutePinConflict(target.Ref())
			}
			if current := st.GlobalRoute(target.Name); current != nil && current.ID != target.ID {
				return nil, proxyFailure("request_conflict")
			}
		}
		var ids map[string]string
		known, e := j.Get("remove_ids", &ids)
		if e != nil {
			return nil, e
		}
		if !known {
			ids = map[string]string{}
			for _, target := range targets {
				ids[target.ID] = tasks.ID()
			}
			if e := j.Set("remove_ids", ids); e != nil {
				return nil, e
			}
		}
		changed := false
		outcomes := []map[string]any{}
		for i, target := range targets {
			ref := target.Ref()
			result, e := (netops.Store{Data: m.Data}).Apply(ctx, ids[target.ID], target, []string{"route/" + ref.Key()}, func(child *netops.Journal) (any, *protocol.Error) {
				var removed string
				if known, e := child.Get("removed", &removed); e != nil {
					return nil, e
				} else if known {
					return map[string]any{"changed": true}, nil
				}
				check := func(st *ports.State) *protocol.Error {
					current := st.GlobalRoute(target.Name)
					if current == nil || current.ID != target.ID {
						return cleanupConflict()
					}
					return m.portStore().Active(ctx, "global-"+target.ID, "backend")
				}
				proof, e := m.disableRoute(ctx, ref, child, check)
				if e != nil {
					return nil, e
				}
				_, e = m.cleanupDisabledRoute(ctx, ref, proof, hm.ForRequest(id, "remove", ref, ""))
				if e != nil {
					return nil, cleanupFailure(e, ref, proof.Changed)
				}
				e = m.portStore().UpdateWithProof(ctx, func(st *ports.State, stamp string) (bool, *protocol.Error) {
					if stamp != proof.After {
						return false, cleanupConflict()
					}
					if e := check(st); e != nil {
						return false, e
					}
					return true, st.RemoveGlobal(target.Name)
				}, func(stamp string) *protocol.Error {
					return child.Set("removed", stamp)
				})
				if e != nil {
					return nil, cleanupFailure(e, ref, proof.Changed)
				}
				return map[string]any{"changed": true}, nil
			})
			if e != nil {
				if changed {
					for _, remaining := range targets[i+1:] {
						outcomes = append(outcomes, map[string]any{"kind": "route", "name": remaining.Name, "id": remaining.ID, "state": "not_attempted", "changed": false})
					}
					e = protocol.WithOutcomes(e, "partial", outcomes...)
				}
				return nil, e
			}
			childChanged := result.(map[string]any)["changed"] == true
			changed = changed || childChanged
			outcomes = append(outcomes, map[string]any{"kind": "route", "name": target.Name, "id": target.ID, "state": "removed", "changed": childChanged})
		}
		return map[string]any{"items": targets, "changed": changed}, nil
	})
}
