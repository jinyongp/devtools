package tasks

import (
	"fmt"
	"strconv"
	"strings"

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
	if e.Code == "revision_conflict" {
		if expected, err := strconv.Atoi(r.Options["if-revision"]); err == nil {
			e.Details["facts"] = map[string]any{"expected_revision": expected}
		}
	}
	ids := arr(Object(e.Details), "ids")
	if (len(ids) == 0 || r.Action == "workstream.edited") && r.Target != "" {
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
		remedies = append(remedies, protocol.LocalAction(argv, nil, "Read the current item and its blocking conditions.", "read").Remedy(protocol.InvocationContext{}))
		if i.Kind == "task" && a.ExecutionStatus == "stale" {
			remedies = append(remedies, protocol.Remedy{Relation: protocol.SameStoreAction, Effect: "value_write", Argv: []string{"devtools", "task", "sync", i.ID, "--profile", profile}, RequiredInputs: []string{"context", "if-revision", "request-id", "reason"}, Message: "Review the changed definition, then sync with the current execution context."})
		}
	}
	if len(remedies) == 0 {
		argv := []string{"devtools", "task", "current"}
		if e.Code == "not_found" {
			argv = []string{"devtools", "task"}
			if d := Find(r.Action); d != nil && d.Kind == "workstream" {
				argv = append(argv, "workstream")
			}
			argv = append(argv, "list")
		}
		remedies = append(remedies, protocol.LocalAction(append(argv, "--profile", profile), nil, "Inspect the current selected scope.", "read").Remedy(protocol.InvocationContext{}))
	}
	if w := s.Items[r.Target]; w != nil && w.Kind == "workstream" && (r.Action == "workstream.edited" || e.Code == "revision_conflict" || e.Code == "validation_required") {
		command := "plan"
		verb := "show"
		if e.Code == "validation_required" {
			command, verb = "check", ""
		}
		argv := []string{"devtools", "task", "workstream", command}
		if verb != "" {
			argv = append(argv, verb)
		}
		argv = append(argv, w.ID, "--profile", profile)
		remedies = append(remedies, protocol.LocalAction(argv, nil, "Inspect the original workstream and its current definition.", "read").Remedy(protocol.InvocationContext{}))
	}
	if e.Code == "invalid_argument" {
		e.Details["remedies"] = remedies
		inputGuidance(r, e)
		remedies = e.Details["remedies"].([]protocol.Remedy)
	}
	checks := []any{}
	for _, issue := range objects(Object(e.Details), "issues") {
		item := s.Items[str(issue, "target_id")]
		if item == nil || str(issue, "code") != "coverage" {
			continue
		}
		checks = append(checks, map[string]any{"id": fmt.Sprintf("task_coverage_%d", len(checks)), "status": "fail", "required": true, "reason": "coverage", "message": str(issue, "message"), "target": map[string]any{"kind": item.Kind, "name": item.ID, "id": item.ID}, "sources": []any{}, "facts": map[string]any{}, "remedies": remedies})
	}
	if len(checks) > 0 {
		e.Details["checks"] = checks
	}
	e.Details["related"] = related
	e.Details["remedies"] = remedies
	if len(related) > 0 {
		e.Details["target"] = related[0]
	}
	return e
}

func inputGuidance(r Request, e *protocol.Error) *protocol.Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	d := Find(r.Action)
	if d == nil {
		return e
	}
	argv := append([]string{"devtools", "schema", "task"}, strings.Fields(d.Command)...)
	// Schema discovery has no selected store. Appending store flags makes those
	// flags part of the discovery target rather than command options.
	guide := protocol.Action{Argv: argv, RequiredInputs: []string{}, Message: "Correct the indicated input location using this command's schema.", Effect: "read", Relation: protocol.DiscoveryAction}.Remedy(protocol.InvocationContext{})
	remedies := []protocol.Remedy{guide}
	// References need the selected plan as well as the input shape.
	reference := false
	if sources, ok := e.Details["sources"].([]any); ok {
		for _, value := range sources {
			if source, ok := value.(map[string]any); ok {
				if path, ok := source["path"].([]string); ok {
					for _, field := range path {
						if field == "id" || strings.HasSuffix(field, "_id") || field == "depends_on" || field == "ref" || field == "requirement_keys" || field == "acceptance_keys" {
							reference = true
						}
					}
				}
			}
		}
	}
	if reference {
		if old, ok := e.Details["remedies"].([]protocol.Remedy); ok {
			remedies = append(remedies, old...)
		}
	}
	e.Details["remedies"] = remedies
	return e
}
