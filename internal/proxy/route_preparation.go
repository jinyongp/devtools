package proxy

import (
	"context"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func routeOutcome(ref network.RouteRef, kind, state string, changed bool) map[string]any {
	name := ref.Proxy
	if name == "" {
		name = ref.RouteID
	}
	return map[string]any{"kind": kind, "name": name, "state": state, "changed": changed}
}

func activationRollbackFailure(original receiptError, recovery *protocol.Error) *protocol.Error {
	recovery = protocol.Public(recovery, "proxy hosts status")
	effects := "partial"
	if original.Details["effects"] == "unknown" || recovery.Details["effects"] == "unknown" {
		effects = "unknown"
	}
	return protocol.NewError("recovery_required", "Route preparation failed and hosts rollback could not be confirmed.", 3, map[string]any{
		"reason": "rollback_incomplete", "effects": effects, "retry": "inspect_first",
		"facts": recovery.Details["facts"],
		"causes": []any{
			map[string]any{"id": "preparation", "code": original.Code, "message": original.Message, "details": original.Details},
			map[string]any{"id": "rollback", "code": recovery.Code, "message": recovery.Message, "details": recovery.Details},
		},
		"primary_cause_id": "preparation",
		"remedies":         []protocol.Remedy{hosts.StatusAction().Remedy(protocol.InvocationContext{})},
	})
}

func routeDNSFailure(issue string) *protocol.Error {
	if issue == "hosts_recovery_required" {
		return protocol.NewError("recovery_required", "The route is waiting for its pending hosts step.", 3, map[string]any{
			"reason": "pending_hosts_operation", "retry": "inspect_first",
			"remedies": []protocol.Remedy{hosts.StatusAction().Remedy(protocol.InvocationContext{})},
		})
	}
	return protocol.NewError(issue, "The route's required local DNS mapping could not be verified.", 3, map[string]any{
		"retry":    "inspect_first",
		"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", "proxy", "list"}, nil, "Inspect the current route and its DNS status.", "read").Remedy(protocol.InvocationContext{})},
	})
}

type listenerPreparation struct {
	Changed    bool
	Generation string
}

func (m Manager) prepareRouteListener(ctx context.Context, name string, attempt *string, save func() *protocol.Error) (listenerPreparation, *protocol.Error) {
	status, e := m.NetworkStatus(ctx, name)
	if e != nil {
		return listenerPreparation{}, e
	}
	completed := (netops.Store{Data: m.Data}).Completed(*attempt)
	if status["running"] != true {
		daemon, e := m.Status(ctx)
		if e != nil {
			return listenerPreparation{}, e
		}
		if daemon.State == "unknown" || daemon.State == "starting" {
			return listenerPreparation{}, proxyFailure("proxy_pending")
		}
		if completed {
			*attempt = tasks.ID()
			if e := save(); e != nil {
				return listenerPreparation{}, e
			}
			completed = false
		}
	} else if !completed {
		return listenerPreparation{}, m.verifyListener(ctx, name, status)
	}
	result, e := m.NetworkApply(ctx, "start", name, false, *attempt)
	if e != nil {
		return listenerPreparation{}, e
	}
	prepared := result.(map[string]any)
	item, _ := prepared["item"].(map[string]any)
	generation, _ := item["generation"].(string)
	return listenerPreparation{Changed: prepared["changed"] == true, Generation: generation}, m.verifyListener(ctx, name, item)
}

func (m Manager) stopUnusedPreparationListener(ctx context.Context, listener network.Listener, generation string, selected map[string]bool) *protocol.Error {
	st, e := m.portStore().Read()
	if e != nil {
		return e
	}
	if current := st.Listener(listener.Name); current == nil || *current != listener || st.ListenerPinned(listener.ID) {
		return nil
	}
	items, e := m.resolver().List(ctx, "")
	if e != nil {
		return e
	}
	for _, item := range items {
		ref := item.Ref()
		if item.Kind == "instance" && item.Status != StatusReady {
			return nil
		}
		if item.Listener != listener.Name || !ref.Valid() {
			continue
		}
		active := st.Activation(ref)
		if active != nil && active.Mode == "active" || !selected[ref.Key()] && item.Status != "inactive" && item.Status != "disabled" {
			return nil
		}
	}
	h, t, e := st.ListenerPorts(listener.Name)
	if e != nil {
		return e
	}
	_, e = m.networkRPC(ctx, networkControlRequest{Action: "stop", Target: listenerTarget{Definition: listener, HTTPPort: h, HTTPSPort: t, AttemptID: generation}})
	if e != nil && (e.Code == "request_conflict" || e.Code == "pinned") {
		return nil
	}
	return e
}

func (m Manager) verifyListener(ctx context.Context, name string, status map[string]any) *protocol.Error {
	st, e := m.portStore().Read()
	if e != nil {
		return e
	}
	listener := st.Listener(name)
	generation, _ := status["generation"].(string)
	if listener == nil || status["id"] != listener.ID || status["running"] != true || !protocol.ValidUUID(generation) {
		return proxyFailure("proxy_pending")
	}
	httpPort, httpsPort, e := st.ListenerPorts(name)
	if e != nil {
		return e
	}
	// The runtime's start request with Create=false only verifies an existing
	// group. Changing sockets or generations requires a distinct start attempt.
	actual, e := m.networkRPC(ctx, networkControlRequest{Action: "start", Target: listenerTarget{
		Definition: *listener, HTTPPort: httpPort, HTTPSPort: httpsPort, AttemptID: generation,
	}})
	if e != nil {
		return e
	}
	if !actual.Running || actual.ID != listener.ID || actual.Generation != generation {
		return proxyFailure("proxy_pending")
	}
	return nil
}

// VerifyRoutePrepared observes the route and its running listener without
// replaying a prior activation or applying a hosts change.
func (m Manager) VerifyRoutePrepared(ctx context.Context, ref network.RouteRef, hm hosts.Manager) *protocol.Error {
	return m.verifyRoutePrepared(ctx, ref, hm, nil)
}

func (m Manager) VerifyActivationPrepared(ctx context.Context, active network.Activation, hm hosts.Manager) *protocol.Error {
	return m.verifyRoutePrepared(ctx, active.Route, hm, &active)
}

func (m Manager) verifyRoutePrepared(ctx context.Context, ref network.RouteRef, hm hosts.Manager, expected *network.Activation) *protocol.Error {
	st, e := m.portStore().Read()
	if e != nil {
		return e
	}
	activation := st.Activation(ref)
	if expected != nil && st.ActivationMatches(*expected) {
		activation = expected
	}
	if activation == nil || activation.Mode != "active" || expected != nil && !st.ActivationMatches(*expected) {
		return proxyFailure("activation_required")
	}
	m.hostSystem = hm.System
	items, e := m.resolver().List(ctx, "")
	if e != nil {
		return e
	}
	for _, item := range items {
		if item.Ref() != ref {
			continue
		}
		if item.DNSIssue != "" {
			return routeDNSFailure(item.DNSIssue)
		}
		if item.Status != StatusReady || item.Listener != activation.Listener {
			return proxyFailure("activation_required")
		}
		status, e := m.NetworkStatus(ctx, item.Listener)
		if e != nil {
			return e
		}
		return m.verifyListener(ctx, item.Listener, status)
	}
	return proxyFailure("route_not_found")
}
