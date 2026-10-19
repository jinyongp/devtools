package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

const AutoRestartWindow = 30 * time.Second
const AutoRestartLimit = 3

// ValueApplyStatus contains coordination metadata only. A persisted request is
// never dispatched twice automatically, including after helper/response loss.
type ValueApplyStatus struct {
	State       string               `json:"state"`
	Reason      string               `json:"reason"`
	UpdatedAt   time.Time            `json:"updated_at"`
	ExecutionID string               `json:"execution_id"`
	RequestID   string               `json:"request_id,omitempty"`
	NextID      string               `json:"next_id,omitempty"`
	Goal        *vault.InputIdentity `json:"goal,omitempty"`
}

func (s Store) readValueApply(id string) (*ValueApplyStatus, error) {
	if !validID(id) {
		return nil, errors.New("invalid execution")
	}
	b, e := maintenance.Read(s.path(id, "value-apply.json"), 8192)
	if e != nil {
		return nil, e
	}
	var v ValueApplyStatus
	if vault.StrictDecode(b, &v) != nil || v.ExecutionID != id {
		return nil, errors.New("invalid apply status")
	}
	switch v.State {
	case "requested", "applied", "failed", "paused":
	default:
		return nil, errors.New("invalid apply state")
	}
	if v.State == "requested" && time.Since(v.UpdatedAt) > 90*time.Second {
		v.State = "paused"
		v.Reason = "coordinator_outcome_unknown"
	}
	return &v, nil
}

// The successor retains its own upstream completion so ordinary retention of an
// old execution cannot disable updates for a long-lived current server.
func (s Store) parentValueApply(r Record) (*ValueApplyStatus, error) {
	b, e := maintenance.Read(s.path(r.ID, "value-parent.json"), 8192)
	if errors.Is(e, os.ErrNotExist) {
		return s.readValueApply(r.AutoOrigin)
	}
	if e != nil {
		return nil, e
	}
	var v ValueApplyStatus
	if vault.StrictDecode(b, &v) != nil || v.ExecutionID != r.AutoOrigin || v.NextID != r.ID {
		return nil, errors.New("invalid parent apply")
	}
	return &v, nil
}

func (s Store) withValueApply(r Record) Record {
	if v, e := s.readValueApply(r.ID); e == nil {
		r.ValueApply = v
	} else if !errors.Is(e, os.ErrNotExist) {
		r.ValueApply = &ValueApplyStatus{State: "paused", Reason: "apply_status_unavailable", ExecutionID: r.ID}
	}
	return r
}

func recentAutoRestarts(history []time.Time, now time.Time) []time.Time {
	out := []time.Time{}
	for _, at := range history {
		if at.After(now.Add(-AutoRestartWindow)) {
			out = append(out, at)
		}
	}
	return out
}

func (s Store) considerValueRestart(ctx context.Context, r Record, observation values.InputObservation) {
	if ctx.Err() != nil || r.StartedAt == nil || r.EndedAt != nil || r.State != "running" || observation.State != "pending" || observation.Current == nil {
		return
	}
	if _, e := s.readValueApply(r.ID); !errors.Is(e, os.ErrNotExist) {
		return
	}
	p, e := project.Resolve(r.Directory, "")
	if e != nil || p.Root != r.Directory || p.Profile != r.Profile || p.Commands[r.Command].ValuesUpdate != "restart" {
		return
	}
	now := time.Now().UTC()
	goal := observation.Current.Identity
	status := ValueApplyStatus{State: "requested", Reason: "restart_requested", UpdatedAt: now, ExecutionID: r.ID, Goal: &goal}
	if r.AutoOrigin != "" {
		previous, e := s.parentValueApply(r)
		if errors.Is(e, os.ErrNotExist) && now.Sub(r.CreatedAt) < 90*time.Second || e == nil && previous.State == "requested" {
			return
		}
		if e != nil || previous.State != "applied" || previous.NextID != r.ID {
			status.State = "paused"
			status.Reason = "previous_apply_failed"
		}
	}
	if len(recentAutoRestarts(r.AutoRestarts, now)) >= AutoRestartLimit {
		status.State = "paused"
		status.Reason = "restart_storm"
	}
	if status.State == "requested" {
		status.RequestID = tasks.ID()
	}
	if writePrivate(s.path(r.ID, "value-apply.json"), status) != nil || status.State != "requested" {
		return
	}
	exe, err := os.Executable()
	if err == nil {
		// Only opaque coordination identifiers enter argv. No values/key/ticket.
		b, _ := json.Marshal(status)
		command := exec.Command(exe, "__values-restart", s.Data, string(b))
		command.Dir = r.Directory
		command.Env, _ = process.SelectParent(os.Environ(), process.ParentPolicy{})
		if r.IsolatedRoot != "" {
			command.Env = append(command.Env, "DEVTOOLS_ISOLATED_ROOT="+r.IsolatedRoot)
		}
		err = process.StartDetached(command)
		if err == nil {
			go command.Wait()
			return
		}
	}
	status.State = "failed"
	status.Reason = "coordinator_start_failed"
	status.UpdatedAt = time.Now().UTC()
	_ = writePrivate(s.path(r.ID, "value-apply.json"), status)
}

// CompleteValueApply updates only the original operation's safe status. It never
// rewrites a supervisor record or signals any process by a stored PID.
func (s Store) CompleteValueApply(request ValueApplyStatus, next string, reason string) error {
	if !validID(request.ExecutionID) || !validID(request.RequestID) {
		return errors.New("invalid apply identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, e := tasks.Lock(ctx, s.path(request.ExecutionID, "value-apply.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	old, e := s.readValueApply(request.ExecutionID)
	if e != nil || old.RequestID != request.RequestID || old.Goal == nil || request.Goal == nil || *old.Goal != *request.Goal {
		return errors.New("apply identity mismatch")
	}
	if old.State != "requested" {
		return nil
	}
	old.UpdatedAt = time.Now().UTC()
	old.NextID = next
	old.State = "applied"
	old.Reason = reason
	if reason != "values_delivered" && reason != "readiness_passed" {
		old.State = "failed"
		switch reason {
		case "process_pending", "launch_outcome_unknown", "lock_pending", "recovery_required", "vault_recovery_required":
			old.State = "paused"
		}
	}
	if validID(next) {
		candidate, e := s.read(next)
		if e == nil && candidate.AutoOrigin == request.ExecutionID && candidate.Previous == request.ExecutionID && candidate.InputVersion != nil && candidate.InputVersion.Identity == *request.Goal {
			if e := writePrivate(s.path(next, "value-parent.json"), old); e != nil {
				return e
			}
		}
	}
	return writePrivate(s.path(request.ExecutionID, "value-apply.json"), old)
}

func (s Store) ReadValueApply(id string) (*ValueApplyStatus, error) { return s.readValueApply(id) }
