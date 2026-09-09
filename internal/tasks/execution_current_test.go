package tasks

import (
	"context"
	"fmt"
	"testing"
)

func currentFixture(t *testing.T) (Store, string, string, string) {
	s := fixture(t)
	w := itemID(call(t, s, "workstream.create", "", Object{"title": "Current"}))
	o := call(t, s, "workstream.edited", w, editBody(
		Object{"op": "spec.update", "value": Object{"body": "spec"}}, Object{"op": "plan.update", "value": Object{"body": "plan"}},
		Object{"op": "requirement.add", "value": Object{"key": "R", "text": "works"}}, Object{"op": "acceptance.add", "value": Object{"key": "A", "text": "passes", "requirement_keys": []string{"R"}}},
		Object{"op": "task.add", "ref": "task", "value": Object{"title": "Work", "acceptance_keys": []string{"A"}}},
		Object{"op": "validation.add", "ref": "check", "value": Object{"title": "Check", "method": "test", "task_id": "@task"}},
	))
	refs := objectValue(o["created_refs"])
	call(t, s, "workstream.activate", w, Object{})
	return s, w, str(refs, "task"), str(refs, "check")
}

func recordPass(t *testing.T, s Store, v, token string) string {
	t.Helper()
	basis := call(t, s, "validation.basis", v, Object{"code": []any{}}, "context", token)
	r := call(t, s, "validation.record", v, Object{"basis_id": basis["basis_id"], "result": "pass", "summary": "passed", "evidence": []any{}}, "context", token)
	return str(r, "record_id")
}

func TestStaleRunSyncAndLateRecord(t *testing.T) {
	s, w, task, v := currentFixture(t)
	claim := call(t, s, "run.claimed", task, Object{})
	token := str(claim, "context")
	basis := call(t, s, "validation.basis", v, Object{"code": []any{}}, "context", token)
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "new behavior"}}))
	late := call(t, s, "validation.record", v, Object{"basis_id": basis["basis_id"], "result": "pass", "summary": "old result", "evidence": []any{}}, "context", token)
	if late["record_id"] == nil {
		t.Fatal(late)
	}
	call(t, s, "run.checkpointed", runID(claim), Object{"summary": "basis changed"}, "context", token)
	_, e := s.Execute(context.Background(), Request{Action: "task.completed", Target: task, Body: Object{"summary": "unsafe"}, Options: map[string]string{"request-id": ID(), "context": token}})
	if e == nil {
		t.Fatal("stale run completed")
	}
	call(t, s, "run.synced", task, Object{"reason": "reviewed new behavior"}, "context", token)
	_, e = s.Execute(context.Background(), Request{Action: "task.completed", Target: task, Body: Object{"summary": "unsafe"}, Options: map[string]string{"request-id": ID(), "context": token}})
	if e == nil {
		t.Fatal("sync invented passing evidence")
	}
	recordPass(t, s, v, token)
	call(t, s, "task.completed", task, Object{"summary": "current"}, "context", token)
	state, _ := s.Read()
	if state.Assessment(task).CompletionStatus != "current" {
		t.Fatal("new completion invalid")
	}
}

func TestDoneStaleClaimIsAtomicAndRemovedRunCanRelease(t *testing.T) {
	s, w, task, v := currentFixture(t)
	claim := call(t, s, "run.claimed", task, Object{})
	token := str(claim, "context")
	recordPass(t, s, v, token)
	call(t, s, "task.completed", task, Object{"summary": "first"}, "context", token)
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "second"}}))
	next := call(t, s, "run.claimed", task, Object{})
	state, _ := s.Read()
	if state.Items[task].State != "open" || state.Current(task) == nil || len(state.definition(task).Completions) != 1 {
		t.Fatal("invalid reopen/claim")
	}
	if _, e := state.AtRevision(num(next, "revision") - 1); e == nil {
		t.Fatal("partial reopen exposed")
	}
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.remove", "id": task}))
	call(t, s, "run.released", runID(next), Object{}, "context", str(next, "context"))
}

func TestWorkstreamEvidenceLossRequiresExplicitReclose(t *testing.T) {
	s, w, task, v := currentFixture(t)
	integration := itemID(call(t, s, "validation.add", "", Object{"title": "Integration", "method": "test", "workstream_id": w}))
	claim := call(t, s, "run.claimed", task, Object{})
	token := str(claim, "context")
	recordPass(t, s, v, token)
	call(t, s, "task.completed", task, Object{"summary": "done"}, "context", token)
	pass := func() {
		state, _ := s.Read()
		basis := call(t, s, "validation.basis", integration, Object{"code": []any{}}, "if-revision", fmt.Sprint(state.Revision))
		call(t, s, "validation.record", integration, Object{"basis_id": basis["basis_id"], "result": "pass", "summary": "integration passed", "evidence": []any{}})
	}
	pass()
	call(t, s, "workstream.close", w, Object{})
	state, _ := s.Read()
	if state.Assessment(w).CompletionStatus != "current" {
		t.Fatal("close invalidated its proof")
	}
	pass()
	state, _ = s.Read()
	if state.Assessment(w).CompletionStatus != "stale" {
		t.Fatal("new evidence automatically reclosed")
	}
	call(t, s, "workstream.close", w, Object{})
	state, _ = s.Read()
	if state.Assessment(w).CompletionStatus != "current" {
		t.Fatal("explicit reclose failed")
	}
}

func TestClosedWorkstreamEvidenceRevocation(t *testing.T) {
	for _, change := range []string{"fail", "blocked", "skipped", "unwaive"} {
		t.Run(change, func(t *testing.T) {
			s, w, task, v := currentFixture(t)
			integration := itemID(call(t, s, "validation.add", "", Object{"title": "Integration", "method": "test", "workstream_id": w}))
			claim := call(t, s, "run.claimed", task, Object{})
			token := str(claim, "context")
			recordPass(t, s, v, token)
			call(t, s, "task.completed", task, Object{"summary": "done"}, "context", token)
			state, _ := s.Read()
			basis := call(t, s, "validation.basis", integration, Object{"code": []any{}}, "if-revision", fmt.Sprint(state.Revision))
			record := func(result string) {
				call(t, s, "validation.record", integration, Object{"basis_id": basis["basis_id"], "result": result, "summary": result, "evidence": []any{}})
			}
			if change == "unwaive" {
				call(t, s, "validation.waive", integration, Object{"reason": "Fixture waiver"})
			} else {
				record("pass")
			}
			call(t, s, "workstream.close", w, Object{})
			if change == "unwaive" {
				call(t, s, "validation.unwaive", integration, Object{"reason": "Withdraw waiver"})
			} else {
				record(change)
			}
			record("pass")
			state, _ = s.Read()
			if state.Assessment(w).CompletionStatus != "stale" {
				t.Fatal("proof restoration automatically reclosed")
			}
			call(t, s, "workstream.close", w, Object{})
			state, _ = s.Read()
			if state.Assessment(w).CompletionStatus != "current" {
				t.Fatal("explicit close failed")
			}
		})
	}
}
