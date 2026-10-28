package lifecycle

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
)

// RecoverRequest restores the original scope, verifies both fingerprints, then
// enters the ordinary request/project locks. Existing children may be observed;
// any new launch still uses the caller's current-configuration preflight.
func (m Manager) RecoverRequest(ctx context.Context, q Request, caller string, mode network.PreparationMode) (Result, bool, *protocol.Error) {
	var out Result
	saved, e := m.InspectRequest(q.RequestID)
	if e != nil {
		if e.Code == "request_not_found" {
			return out, false, nil
		}
		return out, true, e
	}
	scope := saved.Result
	q.Project = project.Context{Profile: scope.Profile, Root: scope.Directory, Source: "file", ConfigPath: filepath.Join(scope.Directory, project.Filename)}
	canonical, err := location.Canonical(caller)
	if err != nil {
		return out, true, services.RequestRecoveryError("project", q.RequestID)
	}
	recovery, intent, e := (netops.Store{Data: m.Data}).ReadExecution(q.RequestID)
	if e != nil {
		return out, true, e
	}
	expected := scope.Directory
	if intent && recovery.Caller != "" {
		expected = recovery.Caller
	}
	if canonical != expected {
		if !intent || recovery.Caller == "" {
			return out, false, nil
		}
		return out, true, services.RequestRecoveryError("project", q.RequestID)
	}
	if intent {
		if e := recovery.Validate(q, mode, canonical); e != nil {
			return out, true, services.RequestRecoveryError("project", q.RequestID)
		}
		q.ProxyMode = len(recovery.Plan.Commands) > 0
		for _, snapshot := range recovery.Plan.Reused {
			q.ProxyMode = q.ProxyMode || snapshot != nil
		}
	} else {
		q.ProxyMode = mode == network.PrepareOn
	}
	value, readErr := m.readReceipt(q.RequestID)
	if readErr != nil {
		return out, true, storageError(readErr)
	}
	needsNetwork := q.Action == ActionUp && q.ProxyMode || q.Action == ActionRestart && value.hasProxyExecutions()
	if !value.complete() && needsNetwork && !value.NetworkPrepared {
		// No process can have been admitted before this preparation barrier.
		// Let the existing valid-config path finish preparing its frozen plan.
		return out, false, nil
	}
	q.RecoverOnly = true
	out, e = m.Apply(ctx, q)
	if e != nil && e.Code == "request_conflict" {
		e = services.RequestRecoveryError("project", q.RequestID)
	}
	return out, true, e
}
