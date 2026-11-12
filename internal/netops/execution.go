package netops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Version   int                                   `json:"version"`
	Mode      network.PreparationMode               `json:"mode"`
	NetworkID string                                `json:"network_id"`
	Commands  []string                              `json:"commands"`
	Reused    map[string]*network.ExecutionSnapshot `json:"reused,omitempty"`
	ReuseIDs  map[string]string                     `json:"reuse_ids,omitempty"`
}

type executionInput struct {
	Action string
	Mode   network.PreparationMode
	Input  any
	Caller string `json:"-"`
}

// ExecutionRecovery contains only immutable selection metadata. Reading it does
// not create a journal, acquire preparation resources or invoke a chooser.
type ExecutionRecovery struct {
	Plan        ExecutionPlan
	Caller      string
	fingerprint string
}

func (r ExecutionRecovery) Validate(input any, mode network.PreparationMode, caller string) *protocol.Error {
	body, e := json.Marshal(executionInput{Action: "execution-request-v1", Mode: mode, Input: input})
	digest := sha256.Sum256(body)
	if e != nil || r.fingerprint != hex.EncodeToString(digest[:]) || r.Plan.Mode != mode || r.Caller != "" && r.Caller != caller {
		return Failure("request_conflict", "Request UUID belongs to different execution input or location.")
	}
	return nil
}

func (s Store) ReadExecution(id string) (ExecutionRecovery, bool, *protocol.Error) {
	var out ExecutionRecovery
	_, intent, e := s.ExecutionKind(id)
	if e != nil || !intent {
		return out, false, e
	}
	var j Journal
	if err := tasks.ReadPrivate(filepath.Join(s.Data, "network-operations", strings.ToLower(id)+".json"), &j); err != nil {
		return out, true, Failure("invalid_storage", "Cannot read execution recovery metadata.")
	}
	found, e := j.Get("execution-plan", &out.Plan)
	if e != nil {
		return out, true, e
	}
	if !j.Done || !found || out.Plan.Version != 1 || !out.Plan.Mode.Valid() || !uuid.MatchString(out.Plan.NetworkID) {
		return out, true, Failure("invalid_storage", "Execution selection has not completed.")
	}
	if _, e := j.Get("execution-caller-v1", &out.Caller); e != nil {
		return out, true, e
	}
	if out.Caller != "" && !filepath.IsAbs(out.Caller) {
		return out, true, Failure("invalid_storage", "Invalid execution recovery location.")
	}
	out.fingerprint = j.Fingerprint
	return out, true, nil
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
	return s.ChooseExecution(ctx, id, input, mode, func() (ExecutionPlan, *protocol.Error) {
		commands, e := choose()
		return ExecutionPlan{Commands: commands}, e
	})
}

// ChooseExecution can retain the exact snapshot of a reused active execution.
func (s Store) ChooseExecution(ctx context.Context, id string, input any, mode network.PreparationMode, choose func() (ExecutionPlan, *protocol.Error), caller ...string) (ExecutionPlan, *protocol.Error) {
	if !mode.Valid() {
		return ExecutionPlan{}, Failure("invalid_argument", "Invalid preparation mode.")
	}
	request := executionInput{Action: "execution-request-v1", Mode: mode, Input: input}
	if len(caller) > 0 {
		request.Caller = caller[0]
	}
	if request.Caller != "" && !filepath.IsAbs(request.Caller) {
		return ExecutionPlan{}, Failure("invalid_argument", "Expected a canonical execution directory.")
	}
	result, e := s.Apply(ctx, id, request, []string{"execution-request/" + strings.ToLower(id)}, func(j *Journal) (any, *protocol.Error) {
		var plan ExecutionPlan
		found, e := j.Get("execution-plan", &plan)
		if e != nil {
			return nil, e
		}
		if !found {
			candidate, e := choose()
			if e != nil {
				return nil, e
			}
			plan = candidate
			plan.Version, plan.Mode, plan.NetworkID = 1, mode, tasks.ID()
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
