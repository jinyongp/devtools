package valuehandoff

import (
	"context"
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

type ReportInput struct {
	Revision            uint64 `json:"revision"`
	RunnerID            string `json:"runner_id"`
	ExternalExecutionID string `json:"external_execution_id"`
	InputID             string `json:"input_id"`
	State               string `json:"state"`
	ExternalRef         string `json:"external_ref,omitempty"`
	ExitCode            *int   `json:"exit_code,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

func (s Store) Report(ctx context.Context, id, requestID string, input ReportInput) (Result, *protocol.Error) {
	if !uuidRE.MatchString(input.RunnerID) || !idRE.MatchString(input.InputID) || !opaque(input.ExternalExecutionID) || input.ExternalRef != "" && !opaque(input.ExternalRef) {
		return Result{}, invalid()
	}
	switch input.Reason {
	case "", "runner_start_failed", "runner_lost", "execution_failed", "canceled", "outcome_unavailable":
	default:
		return Result{}, invalid()
	}
	switch input.State {
	case "started":
		if input.ExternalRef == "" || input.ExitCode != nil || input.Reason != "" {
			return Result{}, invalid()
		}
	case "finished":
		if input.ExternalRef == "" || input.ExitCode == nil {
			return Result{}, invalid()
		}
	case "failed", "unknown":
		if input.Reason == "" || input.ExitCode != nil {
			return Result{}, invalid()
		}
	default:
		return Result{}, invalid()
	}
	return s.controlFiles(ctx, id, requestID, "report", input, input.Revision, nil, func(a *access, r *record) (bool, bool, *protocol.Error) {
		if r.Item.DeliveryID == "" || r.Item.RunnerID != input.RunnerID || r.Item.InputID != input.InputID || r.Item.ExternalExecutionID != input.ExternalExecutionID {
			return false, false, failure("execution_state_conflict")
		}
		runner, err := s.readRunner(ctx, a, input.RunnerID)
		if errors.Is(err, os.ErrNotExist) {
			return false, false, failure("runner_not_found")
		}
		if err != nil {
			return false, false, values.VaultError(ctx, err)
		}
		if !runner.Enabled {
			return false, false, failure("runner_not_found")
		}
		next := "reported_" + input.State
		if input.State == "unknown" {
			next = "unknown"
		}
		same := r.Item.ExecutionState == next && r.Item.ExternalRef == input.ExternalRef && equalExit(r.Item.ExitCode, input.ExitCode) && r.Item.Reason == input.Reason
		if same && r.Item.FactSource == "runner_reported" {
			return false, true, nil
		}
		if r.Item.ExecutionState == "reported_finished" || r.Item.ExecutionState == "reported_failed" || r.Item.ExecutionState == "reported_started" && input.State == "unknown" || r.Item.ExternalRef != "" && input.ExternalRef != r.Item.ExternalRef {
			return false, false, failure("execution_state_conflict")
		}
		r.Item.ExecutionState, r.Item.ExternalRef, r.Item.ExitCode, r.Item.Reason = next, input.ExternalRef, input.ExitCode, input.Reason
		r.Item.FactSource, r.Item.ReporterInstanceID = "runner_reported", runner.InstanceID
		if input.State == "started" || input.State == "finished" {
			r.Item.DeliveryState = "delivered"
		} else if r.Item.DeliveryState == "delivering" {
			r.Item.DeliveryState = "unknown"
		}
		return true, true, nil
	}, func(ctx context.Context, a *access, r record, result Result, files map[string][]byte) error {
		if !uuidRE.MatchString(r.DeliveryRequestID) {
			return vault.ErrIntegrity
		}
		path := s.path("value-execution-requests", r.DeliveryRequestID)
		var original receipt
		if err := s.decode(ctx, a, path, r.DeliveryRequestID, vault.MaterialExecutionRequest, &original); err != nil {
			return err
		}
		if original.Action != "deliver" || original.Result.Item.DeliveryID != r.Item.DeliveryID {
			return vault.ErrIntegrity
		}
		if !original.Pending {
			return nil
		}
		original.Pending, original.Result = false, result
		if r.Item.DeliveryState != "delivered" {
			original.ErrorCode = "delivery_outcome_unknown"
		}
		b, err := s.encode(ctx, a, path, r.DeliveryRequestID, vault.MaterialExecutionRequest, original)
		if err == nil {
			files[path] = b
		}
		return err
	})
}

func equalExit(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
