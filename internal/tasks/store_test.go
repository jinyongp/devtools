package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T) Store {
	t.Helper()
	return Store{Directory: filepath.Join(t.TempDir(), "tasks"), Profile: "test"}
}
func call(t *testing.T, s Store, action, id string, b Object, opts ...string) Object {
	t.Helper()
	options := map[string]string{"request-id": ID()}
	for n := 0; n < len(opts); n += 2 {
		options[opts[n]] = opts[n+1]
	}
	if Find(action).Revision {
		state, e := s.Read()
		if e != nil {
			t.Fatal(e)
		}
		options["if-revision"] = fmt.Sprint(state.Revision)
	}
	o, e := s.Execute(context.Background(), Request{Action: action, Target: id, Body: b, Options: options})
	if e != nil {
		t.Fatalf("%s: %+v", action, e)
	}
	return o
}
func itemID(o Object) string { return str(o["item"].(Object), "id") }
func runID(o Object) string  { return o["run"].(*Run).ID }
func TestClaimRaceAndRecovery(t *testing.T) {
	s := fixture(t)
	id := itemID(call(t, s, "task.add", "", Object{"title": "work"}))
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := []Object{}
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, e := s.Execute(context.Background(), Request{Action: "run.claimed", Target: id, Options: map[string]string{"request-id": ID()}})
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				wins = append(wins, o)
			} else if e.Code != "claim_conflict" {
				t.Errorf("unexpected error: %+v", e)
			}
		}()
	}
	wg.Wait()
	if len(wins) != 1 {
		t.Fatalf("wins %d", len(wins))
	}
	old := wins[0]
	next := call(t, s, "run.taken_over", id, Object{}, "expected-run", runID(old))
	_, e := s.Execute(context.Background(), Request{Action: "run.checkpointed", Target: runID(old), Body: Object{"summary": "old"}, Options: map[string]string{"request-id": ID(), "context": str(old, "context")}})
	if e == nil || e.Code != "context_invalid" {
		t.Fatalf("old context: %+v", e)
	}
	req := Request{Action: "task.completed", Target: id, Body: Object{"summary": "finished"}, Options: map[string]string{"request-id": ID(), "context": str(next, "context")}}
	first, e := s.Execute(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Execute(context.Background(), req)
	if e != nil || again["replayed"] != true || again["context_valid"] != false || num(first, "revision") != num(again, "revision") {
		t.Fatalf("replay: %v %+v", again, e)
	}
	history, e := s.Query(Query{Command: "export", Target: id})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(history)
	if strings.Contains(string(b), str(old, "context")) || strings.Contains(string(b), str(next, "context")) {
		t.Fatal("context leaked")
	}
	call(t, s, "task.reopen", id, Object{"reason": "follow-up"})
}
func TestEmptyClaimReceipt(t *testing.T) {
	s := fixture(t)
	req := Request{Action: "run.claimed", Options: map[string]string{"request-id": ID()}}
	o, e := s.Execute(context.Background(), req)
	if e != nil || o["claimed"] != false {
		t.Fatal(o, e)
	}
	call(t, s, "task.add", "", Object{"title": "later"})
	o, e = s.Execute(context.Background(), req)
	if e != nil || o["claimed"] != false || o["replayed"] != true {
		t.Fatal(o, e)
	}
	if call(t, s, "run.claimed", "", Object{})["claimed"] != true {
		t.Fatal("new claim")
	}
}
func TestWorkstreamValidationAndReopen(t *testing.T) {
	s := fixture(t)
	w := itemID(call(t, s, "workstream.create", "", Object{"title": "Feature"}))
	call(t, s, "spec.set", w, Object{"body": "Build feature", "requirements": []Object{{"key": "R1", "text": "Works"}}, "acceptance": []Object{{"key": "A1", "text": "Passes", "requirement_keys": []string{"R1"}}}})
	id := itemID(call(t, s, "task.add", "", Object{"title": "Implement", "workstream_id": w, "acceptance_keys": []string{"A1"}}))
	v := itemID(call(t, s, "validation.add", "", Object{"title": "Tests", "method": "go test", "task_id": id}))
	call(t, s, "plan.set", w, Object{"body": "Implement and test", "task_ids": []string{id}, "validation_ids": []string{v}})
	call(t, s, "workstream.activate", w, Object{})
	claim := call(t, s, "run.claimed", id, Object{})
	basis := call(t, s, "validation.basis", v, Object{"code": []any{}})
	call(t, s, "validation.record", v, Object{"basis_id": basis["basis_id"], "result": "pass", "summary": "Passed", "evidence": []Object{{"kind": "command", "reference": "go test", "description": "All tests passed"}}}, "context", str(claim, "context"))
	call(t, s, "run.checkpointed", runID(claim), Object{"summary": "Evidence recorded"}, "context", str(claim, "context"))
	call(t, s, "task.completed", id, Object{"summary": "Done"}, "context", str(claim, "context"))
	call(t, s, "workstream.close", w, Object{})
	call(t, s, "workstream.reopen", w, Object{"reason": "Next iteration"})
	state, e := s.Read()
	if e != nil || state.Items[w].State != "draft" || state.Items[id].State != "done" {
		t.Fatal("reopen changed internal results", e)
	}
}
func TestDAGAndSnapshot(t *testing.T) {
	s := fixture(t)
	w := itemID(call(t, s, "workstream.create", "", Object{"title": "One"}))
	v := itemID(call(t, s, "workstream.create", "", Object{"title": "Two"}))
	call(t, s, "workstream.depends", v, Object{"depends_on": []string{w}})
	state, _ := s.Read()
	_, e := s.Execute(context.Background(), Request{Action: "workstream.depends", Target: w, Body: Object{"depends_on": []string{v}}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}})
	if e == nil || e.Code != "dependency_conflict" {
		t.Fatal("cycle accepted", e)
	}
	first, e := s.Query(Query{Command: "workstream list", Options: map[string]string{"limit": "1"}})
	if e != nil {
		t.Fatal(e)
	}
	call(t, s, "workstream.create", "", Object{"title": "Three"})
	next, e := s.Query(Query{Command: "workstream list", Options: map[string]string{"limit": "1", "cursor": str(first, "next_cursor")}})
	if e != nil || num(first, "revision") != num(next, "revision") || next["next_cursor"] != nil {
		t.Fatal("snapshot drift", next, e)
	}
}
func TestStrictJSON(t *testing.T) {
	for _, input := range []string{`{"a":1,"a":2}`, `{} {}`, `[]`, strings.Repeat("[", 70) + strings.Repeat("]", 70)} {
		if _, e := Decode(input); e == nil {
			t.Fatal("accepted", input)
		}
	}
	s := fixture(t)
	_, e := s.Execute(context.Background(), Request{Action: "workstream.create", Body: Object{"title": "x", "state": "done"}, Options: map[string]string{"request-id": ID()}})
	if e == nil {
		t.Fatal("unknown field")
	}
}
