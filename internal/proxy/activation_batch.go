package proxy

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type batchDNS struct {
	Owner             string
	Desired, Previous []hosts.Mapping
}
type batchListener struct {
	Target            listenerTarget
	StartID           string
	PreviouslyRunning bool
	Started           *ListenerStatus
}
type activationBatch struct {
	Routes    []routeCandidate
	DNS       []batchDNS
	Listeners []batchListener
	Rollback  bool
}

// ActivateProjectBatch prepares every DNS mapping and socket before atomically
// activating the routes. Backend assignment is an earlier, independent commit.
func (m Manager) ActivateProjectBatch(ctx context.Context, p project.Context, activations []network.Activation, hm hosts.Manager, id string) *protocol.Error {
	resources := []string{}
	for _, a := range activations {
		resources = append(resources, "route/"+a.Route.Key())
	}
	_, e := (netops.Store{Data: m.Data}).Apply(ctx, id, struct {
		Action string
		Routes []network.Activation
	}{"batch-activate", activations}, resources, func(j *netops.Journal) (any, *protocol.Error) {
		var batch activationBatch
		present, e := j.Get("batch", &batch)
		if e != nil {
			return nil, e
		}
		if !present {
			st, e := m.portStore().Read()
			if e != nil {
				return nil, e
			}
			listeners := map[string]bool{}
			for _, a := range activations {
				if st.Pinned(a.Route) {
					return nil, proxyFailure("pinned")
				}
				definition, ok := p.Proxies[a.Route.Proxy]
				if !ok {
					return nil, proxyFailure("route_not_found")
				}
				listener := st.Listener(a.Listener)
				if listener == nil {
					return nil, proxyFailure("listener_not_found")
				}
				if listener.HTTPSReservation != "" {
					if e := j.ReferenceTLS(listener.ID); e != nil {
						return nil, e
					}
				}
				assignment := st.Get(a.Route.InstanceID, definition.Port)
				if assignment == nil {
					return nil, proxyFailure("assignment_missing")
				}
				candidate := routeCandidate{Activation: a, Listener: *listener, Port: assignment.Port, Service: definition.Port}
				if e := m.validateCandidate(st, candidate, p); e != nil {
					return nil, e
				}
				batch.Routes = append(batch.Routes, candidate)
				desired, e := DNSMapping(a.Host, a.DNS, *listener)
				if e != nil {
					return nil, e
				}
				owner := a.Route.Key() + "/" + listener.ID
				previous, e := hm.OwnerMappings(owner)
				if e != nil {
					return nil, e
				}
				batch.DNS = append(batch.DNS, batchDNS{owner, desired, previous})
				if !listeners[a.Listener] {
					listeners[a.Listener] = true
					h, t, e := st.ListenerPorts(a.Listener)
					if e != nil {
						return nil, e
					}
					status, e := m.NetworkStatus(ctx, a.Listener)
					if e != nil {
						return nil, e
					}
					batch.Listeners = append(batch.Listeners, batchListener{Target: listenerTarget{Definition: *listener, HTTPPort: h, HTTPSPort: t}, StartID: tasks.ID(), PreviouslyRunning: status["running"] == true})
				}
			}
			sort.Slice(batch.Listeners, func(i, j int) bool {
				return batch.Listeners[i].Target.Definition.Name < batch.Listeners[j].Target.Definition.Name
			})
			if e := j.Set("batch", batch); e != nil {
				return nil, e
			}
		}
		rollback := func() *protocol.Error {
			for _, mapping := range batch.DNS {
				current, e := hm.OwnerMappings(mapping.Owner)
				if e != nil {
					return e
				}
				if reflect.DeepEqual(current, mapping.Desired) || len(current) == 0 && len(mapping.Desired) == 0 {
					if len(current) > 0 || len(mapping.Previous) > 0 {
						if e := hm.Sync(context.Background(), mapping.Owner, mapping.Previous, ""); e != nil {
							return e
						}
					}
				}
			}
			items, e := NewResolver(m.portStore()).List(context.Background(), "")
			if e != nil {
				return e
			}
			selected := map[string]bool{}
			for _, route := range batch.Routes {
				selected[route.Activation.Route.Key()] = true
			}
			for i := range batch.Listeners {
				listener := &batch.Listeners[i]
				if listener.PreviouslyRunning || listener.Started == nil {
					continue
				}
				used := false
				for _, item := range items {
					if item.Listener == listener.Target.Definition.Name && item.Status == StatusReady && !selected[item.Ref().Key()] {
						used = true
					}
				}
				if used {
					continue
				}
				target := listener.Target
				target.AttemptID = listener.Started.Generation
				if _, e := m.networkRPC(context.Background(), networkControlRequest{Action: "stop", Target: target}); e != nil && e.Code != "request_conflict" && e.Code != "pinned" {
					return e
				}
				listener.Started = nil
				listener.StartID = tasks.ID()
			}
			batch.Rollback = false
			return j.Set("batch", batch)
		}
		if batch.Rollback {
			if e := rollback(); e != nil {
				return nil, e
			}
		}
		fail := func(original *protocol.Error) (any, *protocol.Error) {
			batch.Rollback = true
			if e := j.Set("batch", batch); e != nil {
				return nil, e
			}
			if e := rollback(); e != nil {
				return nil, protocol.NewError("recovery_required", "Network preparation failed and owned resources still need recovery; retry this request before starting another operation.", 3, map[string]any{"cause": original.Code, "recovery": e.Code})
			}
			return nil, original
		}
		st, e := m.portStore().Read()
		if e != nil {
			return nil, e
		}
		committed := len(batch.Routes) > 0
		for _, route := range batch.Routes {
			current := st.Activation(route.Activation.Route)
			if current == nil || *current != route.Activation {
				committed = false
				if st.Pinned(route.Activation.Route) {
					return nil, proxyFailure("pinned")
				}
			}
		}
		if committed {
			for _, mapping := range batch.DNS {
				if len(mapping.Desired) == 0 && len(mapping.Previous) > 0 {
					if e := hm.Sync(ctx, mapping.Owner, nil, ""); e != nil {
						return nil, e
					}
				}
			}
			for _, route := range batch.Routes {
				if e := hm.RemoveOtherOwners(ctx, route.Activation.Route.Key()+"/", route.Activation.Route.Key()+"/"+route.Listener.ID); e != nil {
					return nil, e
				}
			}
			return map[string]any{"changed": true}, nil
		}
		for _, route := range batch.Routes {
			if e := m.validateCandidate(st, route, p); e != nil {
				return fail(e)
			}
		}
		items, e := NewResolver(m.portStore()).List(ctx, "")
		if e != nil {
			return nil, e
		}
		hostsByName := map[string]string{}
		for _, route := range batch.Routes {
			a := route.Activation
			if old, ok := hostsByName[a.Host]; ok && old != a.Route.Key() {
				return nil, proxyFailure("host_conflict")
			}
			hostsByName[a.Host] = a.Route.Key()
		}
		for _, item := range items {
			if item.Host != nil {
				if owner, ok := hostsByName[*item.Host]; ok && owner != item.Ref().Key() {
					return nil, proxyFailure("host_conflict")
				}
			}
		}
		for _, mapping := range batch.DNS {
			if len(mapping.Desired) > 0 {
				if e := hm.Sync(ctx, mapping.Owner, mapping.Desired, ""); e != nil {
					return fail(e)
				}
			}
		}
		for index := range batch.Listeners {
			listener := &batch.Listeners[index]
			result, e := m.NetworkApply(ctx, "start", listener.Target.Definition.Name, false, listener.StartID)
			if e != nil {
				return fail(e)
			}
			encoded, _ := json.Marshal(result)
			var wrapper struct {
				Item ListenerStatus `json:"item"`
			}
			if json.Unmarshal(encoded, &wrapper) != nil {
				return fail(proxyStorageError())
			}
			listener.Started = &wrapper.Item
			if e := j.Set("batch", batch); e != nil {
				return nil, e
			}
		}
		for _, listener := range batch.Listeners {
			current, e := m.NetworkStatus(ctx, listener.Target.Definition.Name)
			if e != nil {
				return fail(e)
			}
			if listener.Started == nil || current["running"] != true || current["generation"] != listener.Started.Generation {
				return fail(proxyFailure("request_conflict"))
			}
		}
		e = m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			for _, route := range batch.Routes {
				if e := m.validateCandidate(st, route, p); e != nil {
					return false, e
				}
				if e := st.SetActivation(route.Activation); e != nil {
					return false, e
				}
			}
			return true, nil
		})
		if e != nil {
			return fail(e)
		}
		for _, mapping := range batch.DNS {
			if len(mapping.Desired) == 0 && len(mapping.Previous) > 0 {
				if e := hm.Sync(ctx, mapping.Owner, nil, ""); e != nil {
					return nil, e
				}
			}
		}
		for _, route := range batch.Routes {
			if e := hm.RemoveOtherOwners(ctx, route.Activation.Route.Key()+"/", route.Activation.Route.Key()+"/"+route.Listener.ID); e != nil {
				return nil, e
			}
		}
		return map[string]any{"changed": len(batch.Routes) > 0}, nil
	})
	return e
}
