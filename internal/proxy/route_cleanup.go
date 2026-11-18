package proxy

import (
	"context"
	"strings"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
)

type disableProof struct {
	Before, After string
	Changed       bool
}

func cleanupConflict() *protocol.Error {
	return protocol.NewError("request_conflict", "The route or listener changed after this cleanup started. Inspect its current state before issuing a new request.", 3, map[string]any{
		"retry":    "inspect_first",
		"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", "proxy", "list"}, nil, "Inspect the current route selection.", "read").Remedy(protocol.InvocationContext{})},
	})
}

func (m Manager) disableRoute(ctx context.Context, ref network.RouteRef, j *netops.Journal, check func(*ports.State) *protocol.Error) (disableProof, *protocol.Error) {
	key := "disable/" + ref.Key()
	var proof disableProof
	_, e := j.Get(key, &proof)
	if e != nil {
		return proof, e
	}
	e = m.portStore().UpdateWithProof(ctx, func(st *ports.State, stamp string) (bool, *protocol.Error) {
		if check != nil {
			if e := check(st); e != nil {
				return false, e
			}
		}
		if st.Pinned(ref) {
			return false, st.RoutePinConflict(ref)
		}
		current := st.Activation(ref)
		if proof.After != "" {
			if proof.After != stamp || current == nil || current.Mode != "disabled" {
				return false, cleanupConflict()
			}
			return false, nil
		}
		if proof.Before != "" && proof.Before != stamp {
			return false, cleanupConflict()
		}
		proof.Before = stamp
		proof.Changed = current == nil || current.Mode != "disabled"
		if e := j.Set(key, proof); e != nil {
			return false, e
		}
		if !proof.Changed {
			return false, nil
		}
		return true, st.SetActivation(network.Activation{Route: ref, Mode: "disabled"})
	}, func(stamp string) *protocol.Error {
		if proof.After == "" {
			proof.After = stamp
			return j.Set(key, proof)
		}
		return nil
	})
	return proof, e
}

func (m Manager) cleanupDisabledRoute(ctx context.Context, ref network.RouteRef, proof disableProof, hm hosts.Manager) (bool, *protocol.Error) {
	store := m.portStore()
	unlock, e := store.LockRegistry(ctx)
	if e != nil {
		return false, e
	}
	defer unlock()
	stamp, e := store.RegistryStamp()
	if e != nil {
		return false, e
	}
	if stamp != proof.After {
		return false, cleanupConflict()
	}
	result, e := hm.RemovePrefixResult(ctx, ref.Key()+"/")
	return result["changed"] == true, e
}

func cleanupFailure(e *protocol.Error, ref network.RouteRef, changed bool) *protocol.Error {
	effects := "none"
	if changed {
		effects = "partial"
	}
	e = protocol.WithOutcomes(e, effects, routeOutcome(ref, "route", "disabled", changed))
	facts := e.Details["facts"].(map[string]any)
	hostFacts, _ := facts["hosts"].(map[string]any)
	if hostFacts == nil {
		hostFacts = map[string]any{}
		facts["hosts"] = hostFacts
	}
	state := "pending"
	if e.Details["effects"] == "unknown" {
		state = "unknown"
	}
	hostFacts["cleanup_state"] = state
	if e.Code != "request_conflict" {
		e.Message = "The route was disabled. Hosts cleanup is " + state + ". " + strings.TrimSpace(e.Message)
	}
	return e
}
