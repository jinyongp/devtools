package services

import (
	"encoding/json"
	"runtime"

	"github.com/jinyongp/devtools/internal/protocol"
)

// ControlStatus describes capabilities observed through the authenticated live
// endpoint. A persisted capability is never authority to signal a stored PID.
type ControlStatus struct {
	State   string `json:"state"`
	Reason  string `json:"reason"`
	Stop    bool   `json:"stop"`
	Restart bool   `json:"restart"`
	Check   bool   `json:"check"`
}

// CleanupStatus is independent of app exit, readiness and port reachability.
// Complete applies only to Scope, never detached/external resources.
type CleanupStatus struct {
	Scope  string `json:"scope"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

func ownedScope() string {
	if runtime.GOOS == "windows" {
		return "windows_job"
	}
	return "process_group"
}

func initialCleanup() CleanupStatus {
	return CleanupStatus{Scope: ownedScope(), State: "not_requested"}
}

func (r Record) publicContract() Record {
	if r.Mode == "" {
		r.Mode = "managed"
	}
	if r.Cleanup.Scope == "" {
		r.Cleanup = CleanupStatus{Scope: "unknown", State: "unknown", Reason: "legacy_unverified"}
	}
	r.Control = ControlStatus{State: "unavailable", Reason: "controller_unavailable"}
	switch {
	case r.State == "interrupted" || r.Reason == "supervisor_lost":
		r.Control.State, r.Control.Reason = "lost", "controller_lost"
		r.Cleanup.State, r.Cleanup.Reason = "unknown", "controller_lost"
	case r.EndedAt != nil:
		r.Control.State, r.Control.Reason = "ended", "execution_ended"
		r.Control.Restart = r.Command != ""
	case r.controlObserved:
		r.Control = ControlStatus{State: "available", Reason: "authenticated_controller", Stop: true, Restart: r.Command != "", Check: r.ReadyConfigured && r.State == "running"}
	}
	return r
}

// Every record transport exposes the same shape. Saved operation results retain
// their historical observation for exact replay. Only publicContract at a live
// status boundary can establish current authority after decoding stored JSON.
func (r Record) MarshalJSON() ([]byte, error) {
	type recordJSON Record
	public := r.publicContract()
	if !r.controlObserved && r.Control.State != "" && r.EndedAt == nil && r.State != "interrupted" {
		public.Control = r.Control
	}
	return json.Marshal(recordJSON(public))
}

func cleanupUnknown(r Record) *protocol.Error {
	return protocol.NewError("process_cleanup_unknown", "The execution controller is unavailable or cleanup could not be verified. Inspect the execution and its declared ownership scope before starting replacement work.", 3, map[string]any{
		"effects": "unknown", "retry": "inspect_first",
		"target":   map[string]any{"kind": "execution", "id": r.ID, "profile": r.Profile, "directory": r.Directory},
		"facts":    map[string]any{"cleanup": r.publicContract().Cleanup},
		"remedies": []protocol.Remedy{{Relation: protocol.SameStoreAction, Argv: []string{"devtools", "process", "status", r.ID}, Effect: "read", RequiredInputs: []string{}, Message: "Inspect this execution's control and cleanup state."}},
	})
}

func executionConflict(r Record) *protocol.Error {
	condition := failure("process_conflict")
	condition.Message = "The command already has an active execution with a different mode or input selection. Inspect or stop that exact execution before starting another."
	condition.Details = map[string]any{
		"effects": "none", "target": map[string]any{"kind": "execution", "id": r.ID, "profile": r.Profile, "directory": r.Directory},
		"facts": map[string]any{"execution_id": r.ID, "mode": r.Mode},
		"remedies": []protocol.Remedy{
			{Relation: protocol.SameStoreAction, Argv: []string{"devtools", "process", "status", r.ID}, Effect: "read", RequiredInputs: []string{}, Message: "Inspect the existing execution."},
		},
	}
	if r.publicContract().Control.Stop {
		action := protocol.Action{Relation: protocol.SameStoreAction, Effect: "system_write", Message: "Stop the existing execution before starting the selected mode and inputs.", Intent: &protocol.CommandIntent{Command: "process stop", Arguments: []string{r.ID}, NewRequest: true}}
		condition.Details["remedies"] = append(condition.Details["remedies"].([]protocol.Remedy), action.Remedy(protocol.InvocationContext{}))
	}
	return condition
}
