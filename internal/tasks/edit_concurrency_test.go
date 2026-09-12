package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
)

func TestEditWriteFailureAndReceiptRecovery(t *testing.T) {
	s, w, task, _ := currentFixture(t)
	before, _ := os.ReadFile(s.path())
	state, _ := s.Read()
	r := Request{Action: "workstream.edited", Target: w, Body: editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "Changed"}}), Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}}
	s.commit = func(string, any) error { return errors.New("injected atomic replacement failure") }
	if _, e := s.Execute(context.Background(), r); e == nil {
		t.Fatal("write failure accepted")
	}
	after, _ := os.ReadFile(s.path())
	if string(before) != string(after) {
		t.Fatal("failed write changed journal")
	}
	s.commit = nil
	first, e := s.Execute(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.Execute(context.Background(), r)
	if e != nil || retry["replayed"] != true || num(retry, "revision") != num(first, "revision") {
		t.Fatal("receipt recovery", e)
	}
}

func TestCommittedEditErrorRecoversReceipt(t *testing.T) {
	s, w, task, _ := currentFixture(t)
	state, _ := s.Read()
	r := Request{Action: "workstream.edited", Target: w, Body: editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "Committed"}}), Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}}
	s.commit = func(path string, v any) error {
		if e := WritePrivate(path, v); e != nil {
			return e
		}
		return errors.New("injected post-rename error")
	}
	if _, e := s.Execute(context.Background(), r); e == nil {
		t.Fatal("expected uncertain response")
	}
	s.commit = nil
	out, e := s.Execute(context.Background(), r)
	if e != nil || out["replayed"] != true {
		t.Fatal("committed receipt lost", e)
	}
}

func TestEditHonorsMaintenanceGate(t *testing.T) {
	s, w, task, _ := currentFixture(t)
	state, _ := s.Read()
	unlock, e := maintenance.Acquire(context.Background(), maintenance.Root(s.Directory))
	if e != nil {
		t.Fatal(e)
	}
	r := Request{Action: "workstream.edited", Target: w, Body: editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "After restore"}}), Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	_, err := s.Execute(ctx, r)
	cancel()
	unlock()
	if err == nil {
		t.Fatal("edit crossed exclusive maintenance boundary")
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = s.Execute(ctx, r); err != nil {
		t.Fatal("edit deadlocked after maintenance", err)
	}
}

func TestEditRacesWithExecution(t *testing.T) {
	for _, action := range []string{"workstream.edited", "run.claimed", "task.completed", "validation.record", "run.synced", "run.taken_over", "run.revoked", "workstream.close"} {
		t.Run(action, func(t *testing.T) {
			s, w, task, v := currentFixture(t)
			var claim Object
			if action != "run.claimed" {
				claim = call(t, s, "run.claimed", task, Object{})
			}
			token := str(claim, "context")
			basis := ""
			if action == "validation.record" {
				basis = str(call(t, s, "validation.basis", v, Object{"code": []any{}}, "context", token), "basis_id")
			}
			if action == "task.completed" || action == "workstream.close" {
				recordPass(t, s, v, token)
				if action == "workstream.close" {
					call(t, s, "task.completed", task, Object{"summary": "complete"}, "context", token)
				}
			}
			state, _ := s.Read()
			edit := Request{Action: "workstream.edited", Target: w, Body: editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "Concurrent definition"}}), Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}}
			other := Request{Action: action, Target: task, Body: Object{}, Options: map[string]string{"request-id": ID(), "context": token}}
			switch action {
			case "workstream.edited":
				other.Target = w
				other.Body = editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "Competing definition"}})
			case "task.completed":
				other.Body = Object{"summary": "done"}
			case "validation.record":
				other.Target = v
				other.Body = Object{"basis_id": basis, "result": "pass", "summary": "result", "evidence": []any{}}
			case "run.synced":
				other.Body = Object{"reason": "Observed current definition"}
			case "run.taken_over", "run.revoked":
				other.Options["expected-run"] = runID(claim)
				if action == "run.revoked" {
					other.Body = Object{"reason": "revoke"}
				}
			case "workstream.close":
				other.Target = w
			}
			if Find(action).Revision {
				other.Options["if-revision"] = fmt.Sprint(state.Revision)
			}
			var wg sync.WaitGroup
			start := make(chan struct{})
			errs := make(chan error, 2)
			for _, r := range []Request{edit, other} {
				wg.Add(1)
				go func(r Request) {
					defer wg.Done()
					<-start
					_, e := s.Execute(context.Background(), r)
					if e != nil && !contains([]string{"revision_conflict", "transition_conflict", "validation_required", "context_invalid", "claim_conflict", "dependency_conflict", "no_change"}, e.Code) {
						errs <- e
					}
				}(r)
			}
			close(start)
			wg.Wait()
			close(errs)
			for e := range errs {
				t.Fatal(e)
			}
			after, e := s.Read()
			if e != nil {
				t.Fatal(e)
			}
			runs := 0
			for _, r := range after.Runs {
				if r.TaskID == task && r.State == "running" {
					runs++
				}
			}
			if runs > 1 || runs > 0 && after.Items[task].State == "done" {
				t.Fatal("inconsistent claim state")
			}
			if after.Items[task].Description == "Concurrent definition" && after.Assessment(task).CompletionStatus == "current" {
				t.Fatal("old completion applied after edit")
			}
			if after.Items[w].State == "done" && after.Assessment(w).CompletionStatus == "current" && after.Assessment(task).CompletionStatus != "current" {
				t.Fatal("close accepted stale child")
			}
		})
	}
}

func TestABAAndExplicitEvidenceReuse(t *testing.T) {
	s, w, task, v := currentFixture(t)
	claim := call(t, s, "run.claimed", task, Object{})
	token := str(claim, "context")
	pass := recordPass(t, s, v, token)
	takeover := call(t, s, "run.taken_over", task, Object{}, "expected-run", runID(claim))
	token = str(takeover, "context")
	basis := call(t, s, "validation.basis", v, Object{"code": []any{}}, "context", token)
	call(t, s, "validation.accept", v, Object{"basis_id": basis["basis_id"], "record_id": pass, "reason": "Same code and definition"}, "context", token)
	call(t, s, "validation.waive", v, Object{"reason": "Fixture waiver"}, "context", token)
	before, _ := s.Read()
	signature := before.Assessment(task).Signature
	for _, description := range []string{"Changed", ""} {
		call(t, s, "workstream.edited", w, editBody(Object{"op": "task.update", "id": task, "value": Object{"description": description}}))
	}
	after, _ := s.Read()
	if after.Assessment(task).Signature == signature || after.evidenceCurrent(after.Items[v], after.Assessment(task).Signature, runID(takeover)) {
		t.Fatal("ABA revived proof or waiver")
	}
	call(t, s, "run.synced", task, Object{"reason": "Reviewed ABA"}, "context", token)
	newBasis := call(t, s, "validation.basis", v, Object{"code": []any{}}, "context", token)
	_, e := s.Execute(context.Background(), Request{Action: "validation.accept", Target: v, Body: Object{"basis_id": newBasis["basis_id"], "record_id": pass, "reason": "Cannot reuse"}, Options: map[string]string{"request-id": ID(), "context": token}})
	if e == nil {
		t.Fatal("old epoch accepted")
	}
}
