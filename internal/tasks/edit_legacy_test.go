package tasks

import (
	"context"
	"fmt"
	"testing"
)

func TestLegacyEditsKeepRunAndCompletionHistory(t *testing.T) {
	s, w, task, v := currentFixture(t)
	claim := call(t, s, "run.claimed", task, Object{})
	token := str(claim, "context")
	call(t, s, "task.update", task, Object{"title": "Renamed"})
	state, _ := s.Read()
	if state.Assessment(task).ExecutionStatus != "current" {
		t.Fatal("metadata invalidated run")
	}
	recordPass(t, s, v, token)
	call(t, s, "task.completed", task, Object{"summary": "complete"}, "context", token)
	call(t, s, "task.update", task, Object{"description": "New behavior"})
	state, _ = s.Read()
	if state.Items[task].State != "done" || state.Assessment(task).CompletionStatus != "stale" {
		t.Fatal("legacy edit lost completed history")
	}
	_, e := s.Execute(context.Background(), Request{Action: "plan.set", Target: w, Body: Object{"body": "plan", "task_ids": []string{}, "validation_ids": []string{}}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}})
	if e == nil || e.Details["related"] == nil || e.Details["remedies"] == nil {
		t.Fatal("membership omission silently removed work or lacks diagnostics", e)
	}
}

func TestBatchCreationDoesNotReorderLaterClaims(t *testing.T) {
	s := fixture(t)
	w := itemID(call(t, s, "workstream.create", "", Object{"title": "Order"}))
	first := call(t, s, "workstream.edited", w, editBody(
		Object{"op": "task.add", "ref": "one", "value": Object{"title": "One"}},
		Object{"op": "task.add", "ref": "two", "value": Object{"title": "Two"}},
		Object{"op": "task.add", "ref": "three", "value": Object{"title": "Three"}},
	))
	later := itemID(call(t, s, "task.add", "", Object{"title": "Later", "workstream_id": w}))
	state, _ := s.Read()
	for _, id := range objectValue(first["created_refs"]) {
		if state.Items[id.(string)].Order >= state.Items[later].Order {
			t.Fatal("later creation overtook batch")
		}
	}
}
