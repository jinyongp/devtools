package tasks

import (
	"fmt"
	"testing"
)

func TestCurrentQueriesAgreeOnStaleAndRemoved(t *testing.T) {
	s, w, task, v := currentFixture(t)
	claim := call(t, s, "run.claimed", task, Object{})
	token := str(claim, "context")
	recordPass(t, s, v, token)
	call(t, s, "task.completed", task, Object{"summary": "complete"}, "context", token)
	rows, e := s.Query(Query{Command: "list", Options: map[string]string{"completion": "current"}})
	if e != nil || len(rows["items"].([]any)) != 1 {
		t.Fatal(rows, e)
	}
	before, _ := s.Read()
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "changed"}}))
	rows, e = s.Query(Query{Command: "list"})
	if e != nil || len(rows["items"].([]any)) != 1 {
		t.Fatal("stale hidden", rows, e)
	}
	next, e := s.Query(Query{Command: "next"})
	if e != nil || str(objectValue(next["item"]), "id") != task {
		t.Fatal("next disagrees", next, e)
	}
	past, e := s.Query(Query{Command: "workstream plan show", Target: w, Options: map[string]string{"at-revision": fmt.Sprint(before.Revision)}})
	if e != nil || str(past["tasks"].([]Object)[0], "completion_status") != "current" {
		t.Fatal("historical definition lost", e)
	}
	history, e := s.Query(Query{Command: "history", Target: task})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, raw := range history["items"].([]any) {
		if event, ok := raw.(Event); ok && event.Action == "workstream.edited" {
			found = true
		}
	}
	if !found {
		t.Fatal("edit missing from task history")
	}
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.remove", "id": task}))
	rows, e = s.Query(Query{Command: "list"})
	if e != nil || len(rows["items"].([]any)) != 0 {
		t.Fatal("removed task in queue", rows, e)
	}
	rows, e = s.Query(Query{Command: "list", Options: map[string]string{"scope": "removed"}})
	if e != nil || len(rows["items"].([]any)) != 1 {
		t.Fatal("removed done hidden", rows, e)
	}
	show, e := s.Query(Query{Command: "show", Target: task})
	if e != nil || str(objectValue(show["item"]), "scope") != "removed" {
		t.Fatal(show, e)
	}
}

func TestOldProjectionCursorRejected(t *testing.T) {
	s := fixture(t)
	call(t, s, "task.add", "", Object{"title": "one"})
	call(t, s, "task.add", "", Object{"title": "two"})
	page, e := s.Query(Query{Command: "list", Options: map[string]string{"limit": "1"}})
	if e != nil {
		t.Fatal(e)
	}
	token := str(page, "next_cursor")
	id := token[:36]
	var snapshot Snapshot
	path := s.cacheDirectory() + "/" + id + ".json"
	if err := ReadPrivate(path, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Projection = 0
	if err := WritePrivate(path, snapshot); err != nil {
		t.Fatal(err)
	}
	_, e = s.Query(Query{Command: "list", Options: map[string]string{"limit": "1", "cursor": token}})
	if e == nil || e.Code != "cursor_invalid" {
		t.Fatal("old projection accepted", e)
	}
}

func TestCommonEditAppearsInAffectedItemHistory(t *testing.T) {
	s, w, task, v := currentFixture(t)
	result := call(t, s, "workstream.edited", w, editBody(Object{"op": "plan.update", "value": Object{"body": "Changed common plan"}}))
	if len(objectValue(result["impact"])["causes"].([]Object)) < 3 {
		t.Fatal("missing bounded cause paths")
	}
	for _, id := range []string{task, v} {
		state, _ := s.Read()
		last := state.Events[len(state.Events)-1]
		if !eventTouches(last, map[string]bool{id: true}) {
			t.Fatal("indirect edit missing from history", id)
		}
		if !contains(arr(last.Data, "affected_ids"), id) {
			t.Fatal("affected validation missing", id)
		}
	}
}
