package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func editBody(ops ...Object) Object {
	raw, _ := json.Marshal(Object{"reason": "change scope", "operations": ops})
	v, _ := Decode(string(raw))
	return v
}
func TestAtomicEditAddsForwardReferences(t *testing.T) {
	store := fixture(t)
	w := itemID(call(t, store, "workstream.create", "", Object{"title": "Edit"}))
	body := editBody(
		Object{"op": "task.depends.set", "id": "@second", "depends_on": []string{"@first"}},
		Object{"op": "validation.add", "ref": "check", "value": Object{"title": "Check", "method": "test", "task_id": "@second"}},
		Object{"op": "task.add", "ref": "first", "value": Object{"title": "First"}},
		Object{"op": "task.add", "ref": "second", "value": Object{"title": "Second"}},
	)
	s, _ := store.Read()
	request := Request{Action: "workstream.edited", Target: w, Body: body, Options: map[string]string{"if-revision": fmt.Sprint(s.Revision), "dry-run": "true"}}
	before, _ := os.ReadFile(store.path())
	preview, e := store.Execute(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(store.path())
	if string(before) != string(after) || preview["changed"] != false || preview["would_change"] != true {
		t.Fatal("preview mutated", preview)
	}
	delete(request.Options, "dry-run")
	request.Options["request-id"] = ID()
	out, e := store.Execute(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	refs := objectValue(out["created_refs"])
	first, second := str(refs, "first"), str(refs, "second")
	if !validID(first) || !validID(second) {
		t.Fatal(refs)
	}
	s, e = store.Read()
	if e != nil || !contains(s.Items[second].Depends, first) {
		t.Fatal("forward dependency lost", e)
	}
	if len(arr(objectValue(s.Items[w].Props["plan"]), "validation_ids")) != 1 {
		t.Fatal("membership not maintained")
	}
	replay, e := store.Execute(context.Background(), request)
	if e != nil || replay["replayed"] != true || hash(replay["created_refs"]) != hash(refs) {
		t.Fatal("replay drift", e)
	}
}

func TestEditRemovesAndRestoresWithoutResurrectingEdges(t *testing.T) {
	s, w, a, b := assessmentFixture(t)
	applyTestEvent(s, "task.depends", b, Object{"depends_on": []string{a}})
	finishTestTask(s, a)
	finishTestTask(s, b)
	x, e := EvaluateEdit(s, w, editBody(Object{"op": "task.remove", "id": a}), nil)
	if e != nil {
		t.Fatal(e)
	}
	if x.Candidate.Included(x.Candidate.Items[a]) || len(x.Candidate.Items[b].Depends) != 0 || len(x.Candidate.definition(a).Completions) != 1 {
		t.Fatal("bad removal")
	}
	if x.Candidate.Assessment(b).CompletionStatus != "stale" {
		t.Fatal("dependency removal not reflected")
	}
	applyTestEvent(s, "workstream.edited", w, x.Event.Data)
	x, e = EvaluateEdit(s, w, editBody(Object{"op": "task.restore", "id": a}), nil)
	if e != nil {
		t.Fatal(e)
	}
	if !x.Candidate.Included(x.Candidate.Items[a]) || len(x.Candidate.Items[b].Depends) != 0 || x.Candidate.Assessment(a).CompletionStatus != "stale" {
		t.Fatal("restoration revived completion/edges")
	}
}

func TestEditRejectsFinalCyclesAndExplicitRemovedReferences(t *testing.T) {
	s, w, a, b := assessmentFixture(t)
	for _, ops := range [][]Object{
		{{"op": "task.depends.set", "id": a, "depends_on": []string{b}}, {"op": "task.depends.set", "id": b, "depends_on": []string{a}}},
		{{"op": "task.depends.set", "id": b, "depends_on": []string{a}}, {"op": "task.remove", "id": a}},
		{{"op": "task.remove", "id": a}, {"op": "task.depends.set", "id": b, "depends_on": []string{a}}},
		{{"op": "task.remove", "id": a}, {"op": "task.restore", "id": a}},
	} {
		before := hash(s)
		if _, e := EvaluateEdit(s, w, editBody(ops...), nil); e == nil {
			t.Fatal("invalid edit accepted", ops)
		}
		if hash(s) != before {
			t.Fatal("failed edit changed original state")
		}
	}
}

func TestEditCompletedTaskMetadataIsNoOpForEvidence(t *testing.T) {
	s, w, a, _ := assessmentFixture(t)
	finishTestTask(s, a)
	x, e := EvaluateEdit(s, w, editBody(Object{"op": "task.update", "id": a, "value": Object{"title": "New name"}}), nil)
	if e != nil {
		t.Fatal(e)
	}
	if x.Candidate.Assessment(a).CompletionStatus != "current" {
		t.Fatal("metadata invalidated completed task")
	}
	applyTestEvent(s, "workstream.edited", w, x.Event.Data)
	x, e = EvaluateEdit(s, w, editBody(Object{"op": "task.update", "id": a, "value": Object{"title": "New name"}}), nil)
	if e != nil || x.Result["would_change"] != false {
		t.Fatal("same definition not a no-op", e, x.Result)
	}
}

func TestEditDocumentRemovalAndExplicitValidationRemoval(t *testing.T) {
	s, w, a, _ := assessmentFixture(t)
	var val string
	for _, v := range s.List("validation") {
		if str(v.Props, "task_id") == a {
			val = v.ID
		}
	}
	x, e := EvaluateEdit(s, w, editBody(Object{"op": "validation.remove", "id": val}, Object{"op": "task.remove", "id": a}, Object{"op": "acceptance.remove", "key": "A1"}), nil)
	if e != nil {
		t.Fatal(e)
	}
	applyTestEvent(s, "workstream.edited", w, x.Event.Data)
	x, e = EvaluateEdit(s, w, editBody(Object{"op": "task.restore", "id": a}, Object{"op": "acceptance.restore", "key": "A1"}), nil)
	if e != nil {
		t.Fatal(e)
	}
	if x.Candidate.Included(x.Candidate.Items[val]) || len(arr(x.Candidate.Items[a].Props, "acceptance_keys")) != 0 {
		t.Fatal("explicitly excluded validation or old links revived")
	}
}

func TestEditEveryLifecycleAndOperationLimit(t *testing.T) {
	for _, state := range []string{"draft", "active", "done", "canceled"} {
		t.Run(state, func(t *testing.T) {
			s, w, a, b := assessmentFixture(t)
			s.Items[w].State = state
			x, e := EvaluateEdit(s, w, editBody(Object{"op": "task.move", "id": a, "after_id": b}), nil)
			if e != nil || x.Candidate.Items[w].State != state || x.Candidate.definition(w).Order[0] != b {
				t.Fatal("lifecycle blocked edit", e)
			}
		})
	}
	s, w, _, _ := assessmentFixture(t)
	ops := []Object{}
	for n := 0; n < 201; n++ {
		ops = append(ops, Object{"op": "plan.update", "value": Object{"body": "unchanged"}})
	}
	if _, e := EvaluateEdit(s, w, editBody(ops...), nil); e == nil {
		t.Fatal("operation limit not enforced")
	}
	if _, e := EvaluateEdit(s, w, editBody(ops[:200]...), nil); e != nil {
		t.Fatal(e)
	}
}

func TestValidationRestoreRetainsDefinitionAndAuditsObligation(t *testing.T) {
	s, w, task, v := currentFixture(t)
	call(t, s, "validation.update", v, Object{"acceptance_keys": []string{"A"}})
	removed := call(t, s, "workstream.edited", w, editBody(Object{"op": "validation.remove", "id": v}))
	if len(removed["effects"].([]Object)) == 0 {
		t.Fatal("required validation removal not visible")
	}
	call(t, s, "workstream.edited", w, editBody(Object{"op": "validation.restore", "id": v}))
	state, _ := s.Read()
	if !state.Included(state.Items[v]) || !contains(arr(state.Items[v].Props, "acceptance_keys"), "A") {
		t.Fatal("validation definition lost")
	}
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.remove", "id": task}))
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "Edited outside scope", "acceptance_keys": []string{"A"}}}))
	state, _ = s.Read()
	if state.Included(state.Items[task]) {
		t.Fatal("excluded update restored task")
	}
}
