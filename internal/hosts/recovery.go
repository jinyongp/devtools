package hosts

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
)

type RecoveryStatus struct {
	State             string            `json:"state"`
	Pending           *bool             `json:"pending"`
	Changes           []MappingChange   `json:"changes"`
	RecoveryAction    string            `json:"recovery_action"`
	Digest            string            `json:"digest,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	OriginRequestID   string            `json:"origin_request_id,omitempty"`
	StepID            string            `json:"step_id,omitempty"`
	RouteRef          *network.RouteRef `json:"route_ref,omitempty"`
	ListenerID        string            `json:"listener_id,omitempty"`
	RecoveryRequestID string            `json:"recovery_request_id,omitempty"`
}

type MappingChange struct {
	Action string `json:"action"`
	Owner  string `json:"owner"`
	Mapping
}

type recoveryApproval struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Method string `json:"method"`
}

type recoveryPlan struct {
	Status   RecoveryStatus
	Step     *stepInput
	Before   string
	After    string
	Verified string
}

func StatusAction() protocol.Action {
	return protocol.LocalAction([]string{"devtools", "proxy", "hosts", "status"}, nil, "Inspect the pending hosts operation in the same data store.", "read")
}

func staleRecovery() *protocol.Error {
	e := netops.Failure("stale_preview", "Hosts changed since confirmation. Review the current pending hosts operation and use the newly displayed command.")
	e.Details = map[string]any{
		"reason": "hosts_confirmation_changed", "retry": "after_refresh",
		"remedies": []protocol.Remedy{StatusAction().Remedy(protocol.InvocationContext{})},
	}
	return e
}

func (m Manager) RecoveryStatus() RecoveryStatus {
	for range 2 {
		st, e := m.read()
		if e != nil {
			break
		}
		if st.Pending == nil {
			return m.recovery(st, nil, false).Status
		}
		body, busy, e := m.System.observe()
		current, readErr := m.read()
		if readErr != nil {
			break
		}
		if stateDigest(st) == stateDigest(current) {
			return m.recovery(st, body, e != nil || busy).Status
		}
	}
	return RecoveryStatus{State: "unavailable", Changes: []MappingChange{}, RecoveryAction: "blocked", Reason: "references_unavailable"}
}

func (m Manager) token(values ...any) string {
	root, _ := filepath.Abs(m.Data)
	encoded, _ := json.Marshal(append([]any{root}, values...))
	return digest(encoded)
}

func (m Manager) recovery(st state, body []byte, unavailable bool) recoveryPlan {
	pendingExists := st.Pending != nil
	out := recoveryPlan{Status: RecoveryStatus{State: "none", Pending: &pendingExists, Changes: []MappingChange{}, RecoveryAction: "none"}}
	p := st.Pending
	if p == nil {
		return out
	}
	info := &out.Status
	if p.Step != nil && validPending(st) {
		info.OriginRequestID = p.Step.Intent.publicOrigin()
		info.StepID = p.Step.ID
		info.ListenerID = p.Step.Intent.Listener
		if p.Step.Intent.Route.Valid() {
			info.RouteRef = &p.Step.Intent.Route
		}
	}
	compare := func(action string, from, to []Owner) {
		index := indexOwners(to)
		for _, owner := range from {
			for _, mapping := range owner.Mappings {
				if !index[ownerMapping{owner.Key, mapping}] {
					info.Changes = append(info.Changes, MappingChange{action, owner.Key, mapping})
				}
			}
		}
	}
	compare("remove", st.Owners, p.Owners)
	compare("add", p.Owners, st.Owners)
	info.RecoveryAction = "blocked"
	if unavailable {
		info.State, info.Reason = "unavailable", "hosts_unavailable"
		return out
	}
	hash := digest(body)
	info.State = "diverged"
	if hash == p.After {
		info.State = "after"
	} else if hash == p.Request.Expected {
		info.State = "before"
	}
	if !validPending(st) {
		info.Reason = "ownership_unknown"
		return out
	}
	prior, next := mappings(st.Owners), mappings(p.Owners)
	oldBody, oldRender := Render(body, prior)
	newBody, newRender := Render(body, next)
	oldOwned := validateOwnership(body, prior) == nil
	newOwned := validateOwnership(body, next) == nil
	oldExact := oldOwned && oldRender == nil && bytes.Equal(body, oldBody)
	newExact := newOwned && newRender == nil && bytes.Equal(body, newBody)
	out.Before, out.After, out.Step = hash, hash, p.Step
	switch {
	case info.State == "before" && oldOwned && newRender == nil && digest(newBody) == p.After:
		info.RecoveryAction, out.After = "apply_saved", p.After
	case info.State == "after" && newExact:
		info.RecoveryAction = "finalize"
	case info.State == "diverged" && oldExact && !newExact && newRender == nil:
		info.RecoveryAction, out.After = "rebase", digest(newBody)
	case info.State == "diverged" && newExact && !oldExact:
		info.RecoveryAction = "finalize"
	default:
		info.Reason = "divergence_ambiguous"
		if !oldOwned && !newOwned {
			info.Reason = "ownership_unknown"
		} else if (oldRender != nil && oldRender.Code == "hosts_conflict") || (newRender != nil && newRender.Code == "hosts_conflict") {
			info.Reason = "mapping_conflict"
		}
		return out
	}
	if info.RecoveryAction != "finalize" {
		if m.Isolated {
			info.RecoveryAction, info.Reason = "blocked", "isolated_write"
			return out
		}
		if privilege.CheckRequest("hosts", Request{Expected: hash, Mappings: next}) != nil {
			info.RecoveryAction, info.Reason = "blocked", "helper_input_too_large"
			return out
		}
	}
	info.Digest = m.token("hosts-recovery", hash, stateDigest(st), info.RecoveryAction, out.After)
	if p.Approval != nil && info.State != "diverged" {
		info.RecoveryRequestID, info.Digest = p.Approval.ID, p.Approval.Digest
	}
	return out
}

func (m Manager) Recover(ctx context.Context, id, token string) (any, *protocol.Error) {
	id = strings.ToLower(id)
	store := netops.Store{Data: m.Data}
	if !store.Completed(id) {
		if st, e := m.read(); e == nil && st.Pending != nil && st.Pending.Step != nil {
			if st.Pending.Step.ownsRequest(id) {
				return nil, netops.Failure("invalid_argument", "Use a recovery UUID distinct from the original request and hosts step.")
			}
		}
	}
	input := struct{ Action, Token string }{"hosts-recover", token}
	return store.Apply(ctx, id, input, nil, func(j *netops.Journal) (any, *protocol.Error) {
		var plan recoveryPlan
		approved, e := j.Get("recovery", &plan)
		if e != nil {
			return nil, e
		}
		if !approved {
			st, e := m.read()
			if e != nil {
				return nil, e
			}
			if st.Pending == nil {
				return nil, staleRecovery()
			}
			plan.Step = st.Pending.Step
		}
		if plan.Step != nil && plan.Step.ownsRequest(id) {
			return nil, netops.Failure("invalid_argument", "Use a recovery UUID distinct from the original request and hosts step.")
		}
		effect := "unknown"
		apply := func(child *netops.Journal) (any, *protocol.Error) {
			root, unlock, e := m.lock(ctx)
			if e != nil {
				return nil, e
			}
			defer unlock()
			st, e := m.read()
			if e != nil {
				return nil, e
			}
			if st.Pending == nil {
				var verified map[string]any
				found, e := child.Get("verified", &verified)
				if e != nil {
					return nil, e
				}
				var published string
				stamp, e := child.Get("published", &published)
				if e != nil {
					return nil, e
				}
				if approved && found && stamp && referenceStamp(root) == published && verified["references_digest"] == stateDigest(st) {
					verified["publication_stamp"] = published
					return verified, nil
				}
				if approved {
					return nil, recoveryFailure("outcome_unconfirmed", nil)
				}
				return nil, staleRecovery()
			}
			if !sameStep(plan.Step, st.Pending.Step) {
				return nil, staleRecovery()
			}
			body, busy, e := m.System.observe()
			current := m.recovery(st, body, e != nil || busy)
			if current.Status.Digest == "" || current.Status.Digest != token ||
				current.Status.RecoveryRequestID != "" && current.Status.RecoveryRequestID != id {
				if approved && current.Status.State == "unavailable" {
					return nil, recoveryFailure("outcome_unconfirmed", st.Pending)
				}
				return nil, staleRecovery()
			}
			if !approved {
				plan = current
				if e := j.Set("recovery", plan); e != nil {
					return nil, e
				}
				approved = true
			}
			if current.Status.RecoveryAction == "rebase" || current.Status.RecoveryAction == "apply_saved" && st.Pending.Approval == nil {
				st.Pending.Approval = &recoveryApproval{id, token, plan.Status.RecoveryAction}
				st.Pending.Request.Expected, st.Pending.After = current.Before, current.After
				if e := writeState(root, st); e != nil {
					return nil, e
				}
			}
			if current.Status.RecoveryAction == "finalize" {
				effect = "not_performed"
				if plan.Status.RecoveryAction != "finalize" {
					effect = "unknown"
				}
				// A confirmed external edit may have preserved the planned
				// projection while changing the complete file digest.
				st.Pending.After = digest(body)
			} else {
				effect = "performed"
			}
			return m.finishPending(ctx, root, st, body, child)
		}
		if plan.Step == nil {
			_, e = apply(j)
		} else {
			_, e = store.Apply(ctx, plan.Step.ID, *plan.Step, nil, apply)
		}
		if e != nil {
			return nil, e
		}
		if !approved {
			return nil, staleRecovery()
		}
		if plan.Status.RecoveryAction == "finalize" {
			effect = "not_performed"
		}
		item := map[string]any{"state": "resolved", "method": plan.Status.RecoveryAction, "os_write": effect}
		if plan.Status.OriginRequestID != "" {
			item["origin_request_id"] = plan.Status.OriginRequestID
		}
		if plan.Status.StepID != "" {
			item["step_id"] = plan.Status.StepID
		}
		return map[string]any{"item": item, "changed": true, "request_id": id}, nil
	})
}

func sameStep(a, b *stepInput) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID && reflect.DeepEqual(a.Intent, b.Intent) && a.Owner == b.Owner && a.Expected == b.Expected &&
		slices.Equal(a.Desired, b.Desired) && reflect.DeepEqual(a.Guard, b.Guard)
}

func (s stepInput) ownsRequest(id string) bool {
	return id == s.ID || id == s.Intent.RequestID || id == s.Intent.publicOrigin()
}
