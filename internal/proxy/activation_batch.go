package proxy

import (
	"context"
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
	Published         *hosts.Publication
	Undone            bool
}
type batchListener struct {
	Target            listenerTarget
	StartID           string
	PreviouslyRunning bool
	Started           *ListenerStatus
}
type activationBatch struct {
	Routes    []routeCandidate
	Disabled  []network.Activation
	DNS       []batchDNS
	Listeners []batchListener
	Rollback  bool
	Attempt   string
	Cause     *receiptError
	UndoProof *hosts.Publication
}

// ActivateProjectBatch prepares every DNS mapping and socket before atomically
// activating the routes. Backend assignment is an earlier, independent commit.
func (m Manager) ActivateProjectBatch(ctx context.Context, p project.Context, activations []network.Activation, hm hosts.Manager, id string, preserveDisabled ...bool) *protocol.Error {
	m.hostSystem = hm.System
	guardDisabled := len(preserveDisabled) > 0 && preserveDisabled[0]
	resources := []string{}
	for _, a := range activations {
		resources = append(resources, "route/"+a.Route.Key())
	}
	result, e := (netops.Store{Data: m.Data}).Apply(ctx, id, struct {
		Action           string
		Routes           []network.Activation
		PreserveDisabled bool `json:"preserve_disabled,omitempty"`
	}{"batch-activate", activations, guardDisabled}, resources, func(j *netops.Journal) (any, *protocol.Error) {
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
				current := st.Activation(a.Route)
				if a.Mode == "disabled" {
					if current == nil || *current != a {
						return nil, proxyFailure("network_snapshot_changed")
					}
					batch.Disabled = append(batch.Disabled, a)
					continue
				}
				if guardDisabled && current != nil && current.Mode == "disabled" {
					return nil, proxyFailure("network_snapshot_changed")
				}
				if st.Pinned(a.Route) && !st.ActivationMatches(a) {
					return nil, st.RoutePinConflict(a.Route)
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
				batch.DNS = append(batch.DNS, batchDNS{Owner: owner, Desired: desired, Previous: previous})
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
		hostsStep := func(owner, phase string) hosts.Manager {
			phase += "/" + batch.Attempt
			for _, route := range batch.Routes {
				ref := route.Activation.Route
				if ref.Key()+"/"+route.Listener.ID == owner {
					return hm.ForRequest(id, phase, ref, route.Listener.ID)
				}
			}
			return hm.ForRequest(id, phase, network.RouteRef{}, "")
		}
		rollback := func() *protocol.Error {
			for i := len(batch.DNS) - 1; i >= 0; i-- {
				mapping := &batch.DNS[i]
				if mapping.Published == nil || mapping.Undone {
					continue
				}
				if batch.UndoProof == nil {
					batch.UndoProof = mapping.Published
				}
				result, e := hostsStep(mapping.Owner, "batch-rollback").IfPublished(batch.UndoProof).Change(ctx, mapping.Owner, mapping.Previous, "")
				if e != nil {
					return e
				}
				mapping.Undone = true
				if publication := hosts.Published(result); publication != nil {
					batch.UndoProof = publication
				}
				if e := j.Set("batch", batch); e != nil {
					return e
				}
			}
			items, e := m.resolver().List(ctx, "")
			if e != nil {
				return e
			}
			st, e := m.portStore().Read()
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
					ref := item.Ref()
					active := st.Activation(ref)
					if item.Listener == listener.Target.Definition.Name && ref.Valid() &&
						(active != nil && active.Mode == "active" || !selected[ref.Key()] && item.Status != "inactive" && item.Status != "disabled") {
						used = true
					}
				}
				if used || st.ListenerPinned(listener.Target.Definition.ID) {
					continue
				}
				target := listener.Target
				target.AttemptID = listener.Started.Generation
				if _, e := m.networkRPC(ctx, networkControlRequest{Action: "stop", Target: target}); e != nil && e.Code != "request_conflict" && e.Code != "pinned" {
					return e
				}
				listener.Started = nil
				listener.StartID = tasks.ID()
				if e := j.Set("batch", batch); e != nil {
					return e
				}
			}
			batch.Rollback = false
			batch.Attempt = tasks.ID()
			batch.Cause, batch.UndoProof = nil, nil
			for i := range batch.DNS {
				batch.DNS[i].Published, batch.DNS[i].Undone = nil, false
			}
			return j.Set("batch", batch)
		}
		if batch.Rollback {
			if e := rollback(); e != nil {
				if batch.Cause != nil {
					return nil, activationRollbackFailure(*batch.Cause, e)
				}
				return nil, e
			}
		}
		fail := func(original *protocol.Error) (any, *protocol.Error) {
			batch.Rollback = true
			batch.Cause = &receiptError{original.Code, original.Message, original.Details, original.ExitCode}
			if e := j.Set("batch", batch); e != nil {
				return nil, e
			}
			if e := rollback(); e != nil {
				return nil, activationRollbackFailure(*batch.Cause, e)
			}
			return nil, original
		}
		st, e := m.portStore().Read()
		if e != nil {
			return nil, e
		}
		if e := validateDisabledActivations(st, batch.Disabled); e != nil {
			return nil, e
		}
		if len(batch.Routes) == 0 {
			return map[string]any{"changed": false}, nil
		}
		for _, route := range batch.Routes {
			if !st.ActivationMatches(route.Activation) {
				if st.Pinned(route.Activation.Route) {
					return nil, st.RoutePinConflict(route.Activation.Route)
				}
			}
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
		changed := false
		for i := range batch.DNS {
			mapping := &batch.DNS[i]
			if len(mapping.Desired) > 0 {
				result, e := hostsStep(mapping.Owner, "batch-forward").Change(ctx, mapping.Owner, mapping.Desired, "")
				if e != nil {
					// Keep the ordered forward receipts. No compensation may
					// overtake a hosts write whose outcome is still pending.
					if hm.RecoveryStatus().State != "none" {
						if changed {
							e = protocol.WithOutcomes(e, "partial", map[string]any{"kind": "hosts", "name": "Earlier mappings", "state": "committed", "changed": true})
						}
						return nil, e
					}
					return fail(e)
				}
				mapping.Published = hosts.Published(result)
				changed = changed || result["changed"] == true
				if e := j.Set("batch", batch); e != nil {
					return nil, e
				}
			}
		}
		var observation *hosts.Observation
		for _, mapping := range batch.DNS {
			if len(mapping.Desired) > 0 {
				if observation == nil {
					value := hm.Observe()
					observation = &value
				}
				if issue := observation.MappingIssue(mapping.Owner, mapping.Desired[0]); issue != "" {
					return fail(routeDNSFailure(issue))
				}
			}
		}
		for index := range batch.Listeners {
			listener := &batch.Listeners[index]
			name := listener.Target.Definition.Name
			started, e := m.prepareRouteListener(ctx, name, &listener.StartID, func() *protocol.Error {
				return j.Set("batch", batch)
			})
			if e != nil {
				return fail(e)
			}
			if started {
				current, e := m.NetworkStatus(ctx, name)
				if e != nil {
					return fail(e)
				}
				generation, _ := current["generation"].(string)
				listener.Started = &ListenerStatus{Generation: generation, Running: current["running"] == true}
				changed = true
			}
			if e := j.Set("batch", batch); e != nil {
				return nil, e
			}
		}
		for _, listener := range batch.Listeners {
			current, e := m.NetworkStatus(ctx, listener.Target.Definition.Name)
			if e != nil {
				return fail(e)
			}
			if e := m.verifyListener(ctx, listener.Target.Definition.Name, current); e != nil {
				return fail(e)
			}
		}
		activationChanged := false
		e = m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			if e := validateDisabledActivations(st, batch.Disabled); e != nil {
				return false, e
			}
			for _, route := range batch.Routes {
				if current := st.Activation(route.Activation.Route); guardDisabled && current != nil && current.Mode == "disabled" {
					return false, proxyFailure("network_snapshot_changed")
				}
				if e := m.validateCandidate(st, route, p); e != nil {
					return false, e
				}
				if !st.ActivationMatches(route.Activation) {
					if e := st.SetActivation(route.Activation); e != nil {
						return false, e
					}
					activationChanged = true
				}
			}
			return activationChanged, nil
		})
		if e != nil {
			return fail(e)
		}
		for _, mapping := range batch.DNS {
			if len(mapping.Desired) == 0 && len(mapping.Previous) > 0 {
				if e := hostsStep(mapping.Owner, "batch-cleanup").Sync(ctx, mapping.Owner, nil, ""); e != nil {
					return nil, e
				}
			}
		}
		for _, route := range batch.Routes {
			if e := hm.ForRequest(id, "batch-cleanup", route.Activation.Route, route.Listener.ID).RemoveOtherOwners(ctx, route.Activation.Route.Key()+"/", route.Activation.Route.Key()+"/"+route.Listener.ID); e != nil {
				return nil, e
			}
		}
		for _, route := range batch.Routes {
			if e := m.verifyRoutePrepared(ctx, route.Activation.Route, hm, &route.Activation); e != nil {
				return nil, protocol.WithOutcomes(e, "partial", routeOutcome(route.Activation.Route, "route", "committed", activationChanged))
			}
		}
		return map[string]any{"changed": changed || activationChanged}, nil
	})
	if e == nil && result.(map[string]any)["replayed"] == true {
		for _, activation := range activations {
			if activation.Mode == "active" {
				if e := m.verifyRoutePrepared(ctx, activation.Route, hm, &activation); e != nil {
					return e
				}
			}
		}
	}
	return e
}

func validateDisabledActivations(st *ports.State, disabled []network.Activation) *protocol.Error {
	for _, expected := range disabled {
		if current := st.Activation(expected.Route); current == nil || *current != expected {
			return proxyFailure("network_snapshot_changed")
		}
	}
	return nil
}
