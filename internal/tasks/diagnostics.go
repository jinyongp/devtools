package tasks

import (
	"fmt"

	"github.com/jinyongp/devtools/internal/protocol"
)

func contextFailure(reason string) *protocol.Error {
	messages := map[string]string{
		"missing":         "Provide the current execution context.",
		"unknown":         "The execution context is not recognized.",
		"inactive":        "The execution context belongs to a run that is no longer active.",
		"target_mismatch": "The execution context belongs to another task or run.",
	}
	e := failure("context_invalid", messages[reason])
	e.Details = map[string]any{"context_reason": reason}
	return e
}

type targetMismatch struct {
	Expected string
	Actual   string
	Relation string
}

func targetMismatchFailure(mismatch targetMismatch) *protocol.Error {
	e := contextFailure("target_mismatch")
	if mismatch.Relation == "different_owner" {
		e.Message = "The execution context belongs to a different task than the command target."
	} else if mismatch.Expected == mismatch.Actual {
		e.Message = fmt.Sprintf("The execution context and command target refer to different %ss.", mismatch.Expected)
	} else {
		e.Message = fmt.Sprintf("Expected a %s target, but received a %s target.", mismatch.Expected, mismatch.Actual)
	}
	e.Details["expected_target_kind"] = mismatch.Expected
	e.Details["actual_target_kind"] = mismatch.Actual
	return e
}

func (s *State) targetKind(id string) string {
	if s.Runs[id] != nil {
		return "run"
	}
	if item := s.Items[id]; item != nil {
		return item.Kind
	}
	return "unknown"
}

func (s *State) noChangeFailure(r Request, profile string) *protocol.Error {
	e := failure("no_change", "The request would not change any task state.")
	e.Details = map[string]any{"affected_count": 0, "affected_ids": []string{}}
	return s.explain(r, e, profile)
}

func (s *State) explain(r Request, e *protocol.Error, profile string) *protocol.Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details["condition_code"] = e.Code
	e.Details["current_revision"] = s.Revision
	ids := arr(Object(e.Details), "ids")
	if len(ids) == 0 && r.Target != "" {
		ids = []string{r.Target}
	}
	related := []Object{}
	remedies := []protocol.Remedy{}
	for _, id := range unique(ids) {
		i := s.Items[id]
		if run := s.Runs[id]; run != nil {
			i = s.Items[run.TaskID]
		}
		if i == nil {
			continue
		}
		a := s.Assessment(i.ID)
		related = append(related, Object{"id": i.ID, "kind": i.Kind, "title": i.Title, "state": i.State, "completion_status": a.CompletionStatus, "execution_status": a.ExecutionStatus, "blockers": a.Blockers})
		argv := []string{"devtools", "task"}
		if i.Kind == "workstream" {
			argv = append(argv, "workstream")
		}
		if i.Kind == "validation" {
			argv = append(argv, "validation", "show")
		} else {
			argv = append(argv, "context")
		}
		argv = append(argv, i.ID, "--profile", profile)
		remedies = append(remedies, protocol.Remedy{Argv: argv, RequiredInputs: []string{}, Message: "Read the current item and its blocking conditions."})
		if i.Kind == "task" && a.ExecutionStatus == "stale" {
			remedies = append(remedies, protocol.Remedy{Argv: []string{"devtools", "task", "sync", i.ID, "--profile", profile}, RequiredInputs: []string{"context", "if-revision", "request-id", "reason"}, Message: "Review the changed definition, then sync with the current execution context."})
		}
	}
	if len(remedies) == 0 {
		remedies = append(remedies, protocol.Remedy{Argv: []string{"devtools", "task", "current", "--profile", profile}, RequiredInputs: []string{}, Message: "Inspect current execution state."})
	}
	e.Details["related"] = related
	e.Details["remedies"] = remedies
	if len(related) > 0 {
		e.Details["target"] = related[0]
	}
	return e
}
