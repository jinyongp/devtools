package services

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func RequestRecoveryError(group, id string) *protocol.Error {
	return protocol.NewError("request_conflict", "Cannot prove the original request input. Inspect its saved result before deciding on new work.", 3, map[string]any{
		"retry": "inspect_first", "effects": "none",
		"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", group, "request", "show", id}, nil, "Read the saved result without resuming or repeating work.", "read").Remedy(protocol.InvocationContext{})},
	})
}

// RecoverCompleted proves both preparation and mutation fingerprints before
// entering Apply's locked, replay-only path. It never prepares new execution.
// The caller must still enforce its own authorization/profile boundary.
func (s Store) RecoverCompleted(ctx context.Context, q Request, caller string, mode network.PreparationMode) (Result, bool, *protocol.Error) {
	var out Result
	saved, e := s.InspectRequest(q.RequestID)
	if e != nil {
		if e.Code == "request_not_found" {
			return out, false, nil
		}
		return out, true, e
	}
	if saved.State != "completed" || saved.Result == nil {
		return out, false, nil
	}
	r := saved.Result.Item
	if !filepath.IsAbs(r.Directory) || !project.ValidProfile(r.Profile) {
		// Early legacy receipts may lack scope metadata. Their existing
		// valid-configuration path can still prove the original fingerprint.
		return out, false, nil
	}
	recovery, intent, e := (netops.Store{Data: s.Data}).ReadExecution(q.RequestID)
	if e != nil {
		return out, true, e
	}
	if q.Action == "start" {
		canonical, err := location.Canonical(caller)
		if err != nil {
			return out, true, RequestRecoveryError("process", q.RequestID)
		}
		expected := r.Directory
		if intent && recovery.Caller != "" {
			expected = recovery.Caller
		}
		if canonical != expected {
			// Legacy records retain the execution root, not the original caller's
			// subdirectory. Leave their ordinary, valid-config retry path intact.
			if !intent || recovery.Caller == "" {
				return out, false, nil
			}
			return out, true, RequestRecoveryError("process", q.RequestID)
		}
		caller = canonical
		q.Directory = r.Directory
	} else {
		caller = r.Directory
	}
	if intent {
		input := struct {
			Profile, Root string
			Request       Request
		}{r.Profile, r.Directory, q}
		if e := recovery.Validate(input, mode, caller); e != nil {
			return out, true, RequestRecoveryError("process", q.RequestID)
		}
	}
	name := r.Command
	if q.Action == "start" {
		name = q.Command
	}
	if intent {
		if snapshot, reused := recovery.Plan.Reused[name]; reused {
			q.ProxyMode, q.Network = snapshot != nil, snapshot
		} else if len(recovery.Plan.Commands) > 0 {
			q.ProxyMode = true
		}
	} else if q.Action == "restart" && r.ProxyMode || q.Action == "start" && mode == network.PrepareOn {
		q.ProxyMode = true
	} else if mode != network.PrepareAuto && q.Action == "start" {
		return out, true, RequestRecoveryError("process", q.RequestID)
	}
	if q.ProxyMode && q.Network == nil {
		var snapshot network.ExecutionSnapshot
		if err := tasks.ReadPrivate(filepath.Join(s.root(), r.ID, "network.json"), &snapshot); err != nil || snapshot.ID != r.NetworkSnapshotID {
			return out, true, RequestRecoveryError("process", q.RequestID)
		}
		q.Network = &snapshot
	}
	q.ReplayOnly = true
	out, e = s.Apply(ctx, q)
	if e != nil && e.Code == "request_conflict" {
		e = RequestRecoveryError("process", q.RequestID)
	}
	return out, true, e
}
