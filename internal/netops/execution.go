package netops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// ExecutionPlan lives in the journal's extensible private payload. Existing
// supervisors and strict process/lease/receipt readers retain their wire format.
type ExecutionPlan struct {
	Version   int                     `json:"version"`
	Mode      network.PreparationMode `json:"mode"`
	NetworkID string                  `json:"network_id"`
	Commands  []string                `json:"commands"`
}

type executionInput struct {
	Action string
	Mode   network.PreparationMode
	Input  any
}

// ExecutionKind distinguishes new intent journals from legacy prepare-execution
// journals before a caller chooses its replay path. It does not create storage.
func (s Store) ExecutionKind(id string) (exists, intent bool, failure *protocol.Error) {
	if !uuid.MatchString(id) {
		return false, false, Failure("invalid_argument", "A request UUID is required.")
	}
	var journal Journal
	err := tasks.ReadPrivate(filepath.Join(s.Data, "network-operations", strings.ToLower(id)+".json"), &journal)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, Failure("invalid_storage", "Cannot read execution preparation journal.")
	}
	// The marker is written by the same journal transition as its initial intent.
	var version int
	marked, e := journal.Get("execution-intent-version", &version)
	if e != nil {
		return true, false, e
	}
	if marked && version != 1 {
		return true, false, Failure("invalid_storage", "Unsupported execution intent version.")
	}
	var plan ExecutionPlan
	found, e := journal.Get("execution-plan", &plan)
	if e != nil {
		return true, false, e
	}
	if found && (plan.Version != 1 || !plan.Mode.Valid() || !uuid.MatchString(plan.NetworkID)) {
		return true, false, Failure("invalid_storage", "Invalid execution preparation plan.")
	}
	return true, marked || found, nil
}

// PlanExecution freezes canonical request intent and the preparation child UUID.
// Callers launch services only after this returns and all journal locks release.
func (s Store) PlanExecution(ctx context.Context, id string, input any, mode network.PreparationMode, choose func() ([]string, *protocol.Error)) (ExecutionPlan, *protocol.Error) {
	if !mode.Valid() {
		return ExecutionPlan{}, Failure("invalid_argument", "Invalid preparation mode.")
	}
	result, e := s.Apply(ctx, id, executionInput{"execution-request-v1", mode, input}, []string{"execution-request/" + strings.ToLower(id)}, func(j *Journal) (any, *protocol.Error) {
		var plan ExecutionPlan
		found, e := j.Get("execution-plan", &plan)
		if e != nil {
			return nil, e
		}
		if !found {
			commands, e := choose()
			if e != nil {
				return nil, e
			}
			plan = ExecutionPlan{Version: 1, Mode: mode, NetworkID: tasks.ID(), Commands: commands}
			if e := j.Set("execution-plan", plan); e != nil {
				return nil, e
			}
		}
		return map[string]any{"plan": plan}, nil
	})
	if e != nil {
		return ExecutionPlan{}, e
	}
	var response struct {
		Plan ExecutionPlan `json:"plan"`
	}
	b, _ := json.Marshal(result)
	if json.Unmarshal(b, &response) != nil || response.Plan.Version != 1 || response.Plan.Mode != mode || !uuid.MatchString(response.Plan.NetworkID) {
		return ExecutionPlan{}, Failure("invalid_storage", "Invalid execution preparation plan.")
	}
	return response.Plan, nil
}
