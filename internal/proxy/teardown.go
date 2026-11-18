package proxy

import (
	"context"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/truststore"
)

type TeardownSelection struct {
	Listener string `json:"listener"`
	All      bool   `json:"all"`
	RemoveCA bool   `json:"remove_ca"`
}
type TeardownPlan struct {
	Digest        string             `json:"digest"`
	Selection     TeardownSelection  `json:"selection"`
	Listeners     []network.Listener `json:"listeners"`
	Routes        []network.RouteRef `json:"routes"`
	Exposures     []expose.Public    `json:"exposures"`
	CAFingerprint string             `json:"ca_fingerprint"`
	LegacyAttempt string             `json:"legacy_attempt"`
	Runtime       map[string]string  `json:"runtime"`
	HostsOwners   []string           `json:"hosts_owners,omitempty"`
	hostsPlan     []hosts.CleanupStep
	activations   map[string]*network.Activation
}
type TeardownTrust struct {
	Untrust func(context.Context, *ca.Authority) *protocol.Error
	Check   func(context.Context, *ca.Authority) (truststore.TrustStatus, *protocol.Error)
}

func (m Manager) TeardownPreview(ctx context.Context, selection TeardownSelection, hm hosts.Manager) (TeardownPlan, *protocol.Error) {
	plan := TeardownPlan{Selection: selection, Listeners: []network.Listener{}, Routes: []network.RouteRef{}, Exposures: []expose.Public{}, Runtime: map[string]string{}, activations: map[string]*network.Activation{}}
	if selection.All && selection.Listener != "" || selection.RemoveCA && !selection.All {
		return plan, proxyFailure("invalid_argument")
	}
	st, e := m.portStore().Read()
	if e != nil {
		return plan, e
	}
	selected := map[string]bool{}
	if st.Network != nil {
		for _, listener := range st.Network.Listeners {
			if selection.All || listener.Name == network.ListenerName(selection.Listener) {
				selected[listener.Name] = true
				plan.Listeners = append(plan.Listeners, listener)
				status, e := m.NetworkStatus(ctx, listener.Name)
				if e != nil {
					return plan, e
				}
				generation, _ := status["generation"].(string)
				plan.Runtime[listener.ID] = generation
			}
		}
	}
	if len(selected) == 0 {
		if !selection.All && network.ListenerName(selection.Listener) != "default" {
			return plan, proxyFailure("listener_not_found")
		}
		selected["default"] = true
		record, e := m.readRecord()
		if e == nil {
			plan.LegacyAttempt = record.AttemptID
		}
	}
	refs := map[string]network.RouteRef{}
	items, e := NewResolver(m.portStore()).List(ctx, "")
	if e != nil {
		return plan, e
	}
	sources := map[string]string{}
	for _, item := range items {
		if selected[network.ListenerName(item.Listener)] && item.Ref().Valid() {
			refs[item.Ref().Key()] = item.Ref()
			if item.Directory != "" {
				p, e := profilekey.ResolveProject(m.Data, item.Directory, "")
				if e == nil {
					source, e := p.NetworkSources()
					if e == nil {
						sources[item.Directory] = source
					}
				}
			}
		}
	}
	if st.Network != nil {
		for _, activation := range st.Network.Activations {
			if activation.Mode == "active" && selected[activation.Listener] {
				refs[activation.Route.Key()] = activation.Route
			}
		}
	}
	for _, ref := range refs {
		plan.Routes = append(plan.Routes, ref)
		plan.activations[ref.Key()] = st.Activation(ref)
	}
	sort.Slice(plan.Routes, func(i, j int) bool { return plan.Routes[i].Key() < plan.Routes[j].Key() })
	records, e := (expose.Store{Data: m.Data}).List()
	if e != nil {
		return plan, e
	}
	for _, r := range records {
		if selection.All || selected[r.Listener] {
			if item, current := exposureCurrent(r, st); current {
				plan.Exposures = append(plan.Exposures, item)
			}
		}
	}
	if selection.RemoveCA {
		authority, e := (ca.Store{Data: m.Data}).ReadForRemoval()
		if e != nil && e.Code != "ca_not_found" {
			return plan, e
		}
		if authority != nil {
			plan.CAFingerprint = authority.Fingerprint()
		}
	}
	plan.hostsPlan, e = hm.PreviewCleanup(plan.selectsOwner)
	if e != nil {
		return plan, e
	}
	for _, step := range plan.hostsPlan {
		plan.HostsOwners = append(plan.HostsOwners, step.Owner)
	}
	plan.Digest = project.Digest(struct {
		Plan    TeardownPlan
		State   *ports.State
		Records []expose.Record
		Hosts   []hosts.CleanupStep
		Sources map[string]string
	}{plan, st, records, plan.hostsPlan, sources})
	return plan, nil
}

func (p TeardownPlan) selectsOwner(owner string) bool {
	if p.Selection.All {
		return true
	}
	for _, listener := range p.Listeners {
		if strings.HasSuffix(owner, "/"+listener.ID) {
			return true
		}
	}
	return false
}

func teardownChanged(selection TeardownSelection) *protocol.Error {
	argv := []string{"devtools", "proxy", "teardown"}
	if selection.All {
		argv = append(argv, "--all")
	} else if selection.Listener != "" {
		argv = append(argv, "--listener", selection.Listener)
	}
	if selection.RemoveCA {
		argv = append(argv, "--remove-ca")
	}
	return protocol.NewError("stale_preview", "The selected teardown changed. Review the remaining effects and confirm them with a new request UUID.", 3, map[string]any{
		"reason": "hosts_confirmation_changed", "retry": "after_refresh",
		"remedies": []protocol.Remedy{protocol.LocalAction(argv, nil, "Review the current teardown scope.", "read").Remedy(protocol.InvocationContext{})},
	})
}

type teardownProgress struct {
	Done       map[string]bool
	Outcomes   []map[string]any
	RouteStamp string
}

func (m Manager) Teardown(ctx context.Context, selection TeardownSelection, digest, id string, hm hosts.Manager, trust TeardownTrust) (any, *protocol.Error) {
	if selection.RemoveCA && m.IsolatedRoot != "" {
		return nil, proxyFailure("isolated_operation_not_allowed")
	}
	// These are the same resource keys used by listener, exposure and CA
	// operations. Internal phases below avoid nested operation locks.
	resources := []string{"proxy-daemon", "exposures", "ca", "network-teardown"}
	return (netops.Store{Data: m.Data}).Apply(ctx, id, struct {
		Action    string
		Selection TeardownSelection
		Digest    string
	}{"teardown", selection, digest}, resources, func(j *netops.Journal) (result any, problem *protocol.Error) {
		progress := teardownProgress{Done: map[string]bool{}}
		if _, e := j.Get("teardown_phases", &progress); e != nil {
			return nil, e
		}
		changed := func() bool {
			for _, row := range progress.Outcomes {
				if row["changed"] == true {
					return true
				}
			}
			return false
		}
		record := func(key string, outcome map[string]any) *protocol.Error {
			progress.Done[key] = true
			if outcome != nil {
				progress.Outcomes = append(progress.Outcomes, outcome)
			}
			return j.Set("teardown_phases", progress)
		}
		defer func() {
			if problem != nil && len(progress.Outcomes) > 0 {
				effects := "none"
				if changed() {
					effects = "partial"
				}
				problem = protocol.WithOutcomes(problem, effects, progress.Outcomes...)
			}
		}()
		var plan TeardownPlan
		present, e := j.Get("plan", &plan)
		if e != nil {
			return nil, e
		}
		if !present {
			plan, e = m.TeardownPreview(ctx, selection, hm)
			if e != nil {
				return nil, e
			}
			if plan.Digest != digest {
				return nil, teardownChanged(selection)
			}
			if e := j.Set("hosts_plan_v1", plan.hostsPlan); e != nil {
				return nil, e
			}
			if e := j.Set("expected_activations_v1", plan.activations); e != nil {
				return nil, e
			}
			if e := j.Set("plan", plan); e != nil {
				return nil, e
			}
		}
		var hostsPlan []hosts.CleanupStep
		if found, e := j.Get("hosts_plan_v1", &hostsPlan); e != nil {
			return nil, e
		} else if !found {
			return nil, teardownChanged(selection)
		}
		var expected map[string]*network.Activation
		if found, e := j.Get("expected_activations_v1", &expected); e != nil {
			return nil, e
		} else if !found {
			return nil, teardownChanged(selection)
		}
		for _, target := range plan.Exposures {
			key := "exposure/" + target.ID
			if progress.Done[key] {
				continue
			}
			exposure, e := (expose.Store{Data: m.Data}).Read(target.ID)
			if e != nil {
				return nil, e
			}
			if exposure.Generation != target.Generation {
				return nil, proxyFailure("request_conflict")
			}
			if _, stopped, e := m.stopExposure(ctx, exposure); e != nil {
				return nil, e
			} else if e := record(key, map[string]any{"kind": "exposure", "name": target.ID, "id": target.ID, "state": "stopped", "changed": stopped}); e != nil {
				return nil, e
			}
		}
		for _, listener := range plan.Listeners {
			key := "listener/" + listener.ID
			if progress.Done[key] {
				continue
			}
			st, e := m.portStore().Read()
			if e != nil {
				return nil, e
			}
			current := st.Listener(listener.Name)
			if current == nil {
				return nil, cleanupConflict()
			}
			if *current != listener {
				return nil, proxyFailure("request_conflict")
			}
			if st.ListenerPinned(listener.ID) {
				return nil, st.ListenerPinConflict(listener.ID)
			}
			status, e := m.NetworkStatus(ctx, listener.Name)
			if e != nil {
				return nil, e
			}
			generation, _ := status["generation"].(string)
			if generation != plan.Runtime[listener.ID] {
				return nil, cleanupConflict()
			}
			stopped := status["running"] == true
			if stopped {
				h, t, e := st.ListenerPorts(listener.Name)
				if e != nil {
					return nil, e
				}
				if _, e := m.networkRPC(ctx, networkControlRequest{Action: "stop", Target: listenerTarget{Definition: listener, HTTPPort: h, HTTPSPort: t, AttemptID: generation}}); e != nil {
					return nil, e
				}
			}
			if e := record(key, map[string]any{"kind": "listener", "name": listener.Name, "id": listener.ID, "state": "stopped", "changed": stopped}); e != nil {
				return nil, e
			}
		}
		if plan.LegacyAttempt != "" && !progress.Done["legacy"] {
			status, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			_, e = m.stopAttempt(ctx, plan.LegacyAttempt, func(string, string) *protocol.Error { return nil })
			if e != nil {
				return nil, e
			}
			if e := record("legacy", map[string]any{"kind": "listener", "name": "default", "state": "stopped", "changed": status.Running}); e != nil {
				return nil, e
			}
		}
		for _, ref := range plan.Routes {
			key := "route/" + ref.Key()
			if progress.Done[key] {
				continue
			}
			proof, e := m.disableRoute(ctx, ref, j, func(st *ports.State) *protocol.Error {
				current, previous := st.Activation(ref), expected[ref.Key()]
				if current == nil && previous == nil || current != nil && previous != nil && *current == *previous {
					return nil
				}
				var existing disableProof
				if _, e := j.Get("disable/"+ref.Key(), &existing); e != nil {
					return e
				}
				if existing.After != "" && current != nil && current.Mode == "disabled" {
					return nil
				}
				return cleanupConflict()
			})
			if e != nil {
				return nil, e
			}
			progress.RouteStamp = proof.After
			if e := record(key, routeOutcome(ref, "route", "disabled", proof.Changed)); e != nil {
				return nil, e
			}
		}
		for _, step := range hostsPlan {
			key := "hosts/" + step.Owner
			if progress.Done[key] {
				continue
			}
			store := m.portStore()
			unlock, e := store.LockRegistry(ctx)
			if e != nil {
				return nil, e
			}
			stamp, e := store.RegistryStamp()
			if e == nil && progress.RouteStamp != "" && stamp != progress.RouteStamp {
				e = cleanupConflict()
			}
			var changedHosts bool
			if e == nil {
				result, err := hm.ForRequest(id, "teardown", network.RouteRef{}, "").Change(ctx, step.Owner, nil, step.Token)
				changedHosts, e = result["changed"] == true, err
			}
			unlock()
			if e != nil {
				if e.Code == "stale_preview" {
					e = teardownChanged(selection)
				}
				return nil, e
			}
			if e := record(key, map[string]any{"kind": "hosts", "name": step.Owner, "state": "committed", "changed": changedHosts}); e != nil {
				return nil, e
			}
		}
		if !progress.Done["remove_listeners"] {
			removed := false
			for _, listener := range plan.Listeners {
				status, e := m.NetworkStatus(ctx, listener.Name)
				if e != nil {
					return nil, e
				}
				if status["running"] == true || status["generation"] != plan.Runtime[listener.ID] {
					return nil, cleanupConflict()
				}
			}
			if e := m.portStore().UpdateWithProof(ctx, func(st *ports.State, stamp string) (bool, *protocol.Error) {
				if progress.RouteStamp != "" && stamp != progress.RouteStamp {
					return false, cleanupConflict()
				}
				if st.Network == nil {
					return false, nil
				}
				ids, keys := map[string]bool{}, map[string]bool{}
				for _, listener := range plan.Listeners {
					if current := st.Listener(listener.Name); current != nil {
						if *current != listener {
							return false, proxyFailure("request_conflict")
						}
						ids[listener.ID] = true
						keys[listener.HTTPReservation] = true
						keys[listener.HTTPSReservation] = true
					}
				}
				listeners := []network.Listener{}
				for _, l := range st.Network.Listeners {
					if !ids[l.ID] {
						listeners = append(listeners, l)
					}
				}
				st.Network.Listeners = listeners
				reservations := []ports.Reservation{}
				for _, r := range st.Reservations {
					if !keys[r.Name] {
						reservations = append(reservations, r)
					}
				}
				st.Reservations = reservations
				removed = len(ids) > 0
				return removed, nil
			}, func(string) *protocol.Error {
				return record("remove_listeners", map[string]any{"kind": "listener", "name": "Selected definitions", "state": "removed", "changed": removed})
			}); e != nil {
				return nil, e
			}
		}
		if selection.RemoveCA && plan.CAFingerprint != "" {
			if e := m.noCAReferences(); e != nil {
				return nil, e
			}
			authority, e := (ca.Store{Data: m.Data}).ReadForRemoval()
			if e != nil {
				if e.Code == "ca_not_found" {
					return map[string]any{"item": plan, "changed": true}, nil
				}
				return nil, e
			}
			if authority.Fingerprint() != plan.CAFingerprint {
				return nil, proxyFailure("ca_changed")
			}
			untrust := trust.Untrust
			if untrust == nil {
				untrust = func(ctx context.Context, a *ca.Authority) *protocol.Error {
					return (truststore.Manager{}).Apply(ctx, "untrust", a)
				}
			}
			if e := untrust(ctx, authority); e != nil {
				return nil, e
			}
			check := trust.Check
			if check == nil {
				check = truststore.Check
			}
			status, e := check(ctx, authority)
			if e != nil || !status.Known || status.Trusted {
				return nil, proxyFailure("untrust_unconfirmed")
			}
			if e := m.noCAReferences(); e != nil {
				return nil, e
			}
			if e := (ca.Store{Data: m.Data}).Remove(ctx, plan.CAFingerprint); e != nil {
				return nil, e
			}
		}
		return map[string]any{"item": plan, "changed": changed() || plan.CAFingerprint != ""}, nil
	})
}
func (m Manager) releaseExposure(ctx context.Context, r expose.Record) (bool, *protocol.Error) {
	removed := false
	e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		if st.Network != nil {
			pins := []network.Pin{}
			for _, pin := range st.Network.Pins {
				if pin.ID != r.ID {
					pins = append(pins, pin)
					continue
				}
				expected := r.Pin
				// Historical stopped records omit the forwarding policy.
				if r.State == "stopped" && expected.HostPolicy == "" {
					expected.HostPolicy = pin.HostPolicy
				}
				if pin != expected {
					return false, proxyFailure("request_conflict")
				}
				removed = true
			}
			st.Network.Pins = pins
		}
		reservations := []ports.Reservation{}
		for _, reservation := range st.Reservations {
			if reservation.Name != r.Reservation {
				reservations = append(reservations, reservation)
				continue
			}
			if r.Reservation != "origin."+strings.ReplaceAll(r.ID, "-", "") || r.OriginPort != 0 && reservation.Port != r.OriginPort {
				return false, proxyFailure("request_conflict")
			}
			removed = true
		}
		st.Reservations = reservations
		return removed, nil
	})
	return removed && e == nil, e
}
func (m Manager) noCAReferences() *protocol.Error {
	st, e := m.portStore().Read()
	if e != nil {
		return e
	}
	if st.Network != nil {
		if len(st.Network.Pins) > 0 {
			return proxyFailure("ca_referenced")
		}
		for _, l := range st.Network.Listeners {
			if l.HTTPSReservation != "" {
				return proxyFailure("ca_referenced")
			}
		}
		for _, a := range st.Network.Activations {
			if a.Mode == "active" {
				return proxyFailure("ca_referenced")
			}
		}
	}
	records, e := (expose.Store{Data: m.Data}).List()
	if e != nil {
		return e
	}
	for _, r := range records {
		if r.State != "stopped" {
			return proxyFailure("ca_referenced")
		}
	}
	return nil
}
