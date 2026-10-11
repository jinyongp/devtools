package proxy

import (
	"context"
	"sort"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
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
}
type TeardownTrust struct {
	Untrust func(context.Context, *ca.Authority) *protocol.Error
	Check   func(context.Context, *ca.Authority) (truststore.TrustStatus, *protocol.Error)
}

func (m Manager) TeardownPreview(ctx context.Context, selection TeardownSelection, hm hosts.Manager) (TeardownPlan, *protocol.Error) {
	plan := TeardownPlan{Selection: selection, Listeners: []network.Listener{}, Routes: []network.RouteRef{}, Exposures: []expose.Public{}, Runtime: map[string]string{}}
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
				p, e := project.Resolve(item.Directory, "")
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
	}
	sort.Slice(plan.Routes, func(i, j int) bool { return plan.Routes[i].Key() < plan.Routes[j].Key() })
	records, e := (expose.Store{Data: m.Data}).List()
	if e != nil {
		return plan, e
	}
	for _, r := range records {
		if r.State != "stopped" && (selection.All || selected[r.Listener]) {
			plan.Exposures = append(plan.Exposures, r.Public)
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
	hostsDigest, e := hm.ReferenceDigest()
	if e != nil {
		return plan, e
	}
	plan.Digest = project.Digest(struct {
		Plan    TeardownPlan
		State   *ports.State
		Records []expose.Record
		Hosts   string
		Sources map[string]string
	}{plan, st, records, hostsDigest, sources})
	return plan, nil
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
	}{"teardown", selection, digest}, resources, func(j *netops.Journal) (any, *protocol.Error) {
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
				return nil, proxyFailure("stale_preview")
			}
			if e := j.Set("plan", plan); e != nil {
				return nil, e
			}
		}
		for _, target := range plan.Exposures {
			record, e := (expose.Store{Data: m.Data}).Read(target.ID)
			if e != nil {
				return nil, e
			}
			if record.Generation != target.Generation {
				return nil, proxyFailure("request_conflict")
			}
			if record.State != "stopped" {
				_, e := m.exposureRPC(ctx, exposureRPCRequest{Action: "stop", ID: record.ID, Generation: record.Generation})
				if e != nil {
					daemon, statusErr := m.Status(ctx)
					if statusErr != nil || daemon.Running {
						return nil, e
					}
					if record.ProviderOwner != "" {
						switch record.Provider {
						case "tailscale":
							e = (expose.Tailscale{Data: m.Data}).RecoverStop(ctx, record)
						case "cloudflared":
							e = (expose.Cloudflared{Data: m.Data}).RecoverStop(ctx, record)
						default:
							e = proxyFailure("provider_recovery_required")
						}
						if e != nil {
							return nil, e
						}
					}
					record.State = "stopped"
					record.Reason = "teardown"
					if e := (expose.Store{Data: m.Data}).Write(record); e != nil {
						return nil, e
					}
				}
			}
			if e := m.releaseExposure(ctx, record); e != nil {
				return nil, e
			}
		}
		for _, listener := range plan.Listeners {
			st, e := m.portStore().Read()
			if e != nil {
				return nil, e
			}
			current := st.Listener(listener.Name)
			if current == nil {
				continue
			}
			if *current != listener {
				return nil, proxyFailure("request_conflict")
			}
			if st.ListenerPinned(listener.ID) {
				return nil, proxyFailure("pinned")
			}
			status, e := m.NetworkStatus(ctx, listener.Name)
			if e != nil {
				return nil, e
			}
			if status["running"] == true {
				generation, _ := status["generation"].(string)
				if generation != plan.Runtime[listener.ID] {
					return nil, proxyFailure("request_conflict")
				}
				h, t, e := st.ListenerPorts(listener.Name)
				if e != nil {
					return nil, e
				}
				if _, e := m.networkRPC(ctx, networkControlRequest{Action: "stop", Target: listenerTarget{Definition: listener, HTTPPort: h, HTTPSPort: t, AttemptID: generation}}); e != nil {
					return nil, e
				}
			}
		}
		if plan.LegacyAttempt != "" {
			if _, e := m.stopAttempt(ctx, plan.LegacyAttempt, func(string, string) *protocol.Error { return nil }); e != nil {
				return nil, e
			}
		}
		for _, ref := range plan.Routes {
			if e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
				st.UpgradeNetwork()
				if st.Pinned(ref) {
					return false, proxyFailure("pinned")
				}
				if a := st.Activation(ref); a != nil && a.Mode == "disabled" {
					return false, nil
				}
				return true, st.SetActivation(network.Activation{Route: ref, Mode: "disabled"})
			}); e != nil {
				return nil, e
			}
			if e := hm.RemovePrefix(ctx, ref.Key()+"/"); e != nil {
				return nil, e
			}
		}
		if e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
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
			return len(ids) > 0, nil
		}); e != nil {
			return nil, e
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
		return map[string]any{"item": plan, "changed": len(plan.Listeners) > 0 || len(plan.Routes) > 0 || len(plan.Exposures) > 0 || plan.CAFingerprint != ""}, nil
	})
}
func (m Manager) releaseExposure(ctx context.Context, r expose.Record) *protocol.Error {
	return m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		if st.Network == nil {
			return false, nil
		}
		pins := []network.Pin{}
		removed := false
		for _, pin := range st.Network.Pins {
			if pin.ID == r.ID {
				if pin != r.Pin {
					return false, proxyFailure("request_conflict")
				}
				removed = true
			} else {
				pins = append(pins, pin)
			}
		}
		st.Network.Pins = pins
		reservations := []ports.Reservation{}
		for _, reservation := range st.Reservations {
			if reservation.Name == r.Reservation {
				removed = true
			} else {
				reservations = append(reservations, reservation)
			}
		}
		st.Reservations = reservations
		return removed, nil
	})
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
