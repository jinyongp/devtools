package tasks

import "github.com/jinyongp/devtools/internal/protocol"

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
	remedies := []Object{}
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
		remedies = append(remedies, Object{"argv": argv, "required_inputs": []string{}, "message": "Read the current item and its blocking conditions."})
		if i.Kind == "task" && a.ExecutionStatus == "stale" {
			remedies = append(remedies, Object{"argv": []string{"devtools", "task", "sync", i.ID, "--profile", profile}, "required_inputs": []string{"context", "if-revision", "request-id", "reason"}, "message": "Review the changed definition, then sync with the current execution context."})
		}
	}
	if len(remedies) == 0 {
		remedies = append(remedies, Object{"argv": []string{"devtools", "task", "current", "--profile", profile}, "required_inputs": []string{}, "message": "Inspect current execution state."})
	}
	e.Details["related"] = related
	e.Details["remedies"] = remedies
	if len(related) > 0 {
		e.Details["target"] = related[0]
	}
	return e
}
