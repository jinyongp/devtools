package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

func TestStandaloneWorkstreamMetadataUpdateUsesCanonicalEdit(t *testing.T) {
	s := fixture(t)
	w := itemID(call(t, s, "workstream.create", "", Object{"title": "Original", "description": "Goal"}))
	state, e := s.Read()
	if e != nil {
		t.Fatal(e)
	}
	request := Request{Action: "workstream.update", Target: w, Body: Object{"title": "Renamed", "description": "New goal"}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}}
	out, e := s.Execute(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	if str(out["item"].(Object), "title") != "Renamed" || str(out["item"].(Object), "description") != "New goal" {
		t.Fatal("metadata update missing", out)
	}
	replayed, e := s.Execute(context.Background(), request)
	if e != nil || replayed["replayed"] != true {
		t.Fatal("metadata update retry did not replay", replayed, e)
	}
	state, e = s.Read()
	if e != nil || state.Events[len(state.Events)-1].Action != "workstream.edited" {
		t.Fatal("standalone update did not use canonical edit event", e)
	}
	exported, e := s.Query(Query{Command: "workstream export", Target: w})
	encoded, _ := json.Marshal(exported)
	if e != nil || !strings.Contains(string(encoded), "Renamed") || !strings.Contains(string(encoded), "workstream.edited") {
		t.Fatal("metadata missing from export or history", e, string(encoded))
	}
	state, _ = s.Read()
	_, e = s.Execute(context.Background(), Request{Action: "workstream.update", Target: w, Body: Object{"title": "Renamed"}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}})
	if e == nil || e.Code != "no_change" {
		t.Fatal("identical standalone update did not fail loudly", e)
	}
	task := itemID(call(t, s, "task.add", "", Object{"title": "Wrong kind"}))
	state, _ = s.Read()
	_, e = s.Execute(context.Background(), Request{Action: "workstream.update", Target: task, Body: Object{"title": "Rejected"}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}})
	if e == nil || e.Code != "not_found" {
		t.Fatal("task target accepted as workstream", e)
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
