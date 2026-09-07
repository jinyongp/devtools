package tasks

import (
	"context"
	"fmt"
	"testing"
)

func TestAdministrativeUnclaim(t *testing.T) {
	s := fixture(t)
	id := itemID(call(t, s, "task.add", "", Object{"title": "Recover work"}))
	claimed := call(t, s, "run.claimed", id, Object{})
	state, _ := s.Read()
	req := Request{Action: "run.revoked", Target: id, Body: Object{"reason": "Agent session ended"}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision), "expected-run": ID()}}
	if _, e := s.Execute(context.Background(), req); e == nil || e.Code != "claim_conflict" {
		t.Fatalf("unexpected run accepted: %v", e)
	}
	req.Options["expected-run"] = runID(claimed)
	result, e := s.Execute(context.Background(), req)
	if e != nil || result["context"] != nil {
		t.Fatalf("revoke: %v %v", result, e)
	}
	state, _ = s.Read()
	if state.Current(id) != nil || state.Items[id].State != "open" || state.Runs[runID(claimed)].State != "revoked" {
		t.Fatal("revocation must end only the observed run")
	}
	if str(state.Events[len(state.Events)-1].Data, "reason") != "Agent session ended" {
		t.Fatal("missing intervention reason")
	}
	for _, action := range []string{"run.checkpointed", "run.released", "task.completed", "run.resumed"} {
		target := id
		if Find(action).Kind == "run" {
			target = runID(claimed)
		}
		body := Object{}
		if action == "run.checkpointed" || action == "task.completed" {
			body["summary"] = "Late result"
		}
		_, e := s.Execute(context.Background(), Request{Action: action, Target: target, Body: body, Options: map[string]string{"request-id": ID(), "context": str(claimed, "context")}})
		if e == nil || e.Code != "context_invalid" {
			t.Fatalf("late %s: %v", action, e)
		}
	}
	next := call(t, s, "run.claimed", id, Object{})
	if replay, e := s.Execute(context.Background(), req); e != nil || replay["replayed"] != true {
		t.Fatalf("retry: %v %v", replay, e)
	}
	state, _ = s.Read()
	if state.Current(id).ID != runID(next) {
		t.Fatal("retry revoked the new run")
	}
	req.Options["request-id"] = ID()
	if _, e := s.Execute(context.Background(), req); e == nil || e.Code != "revision_conflict" {
		t.Fatalf("stale revision: %v", e)
	}
	req.Options["if-revision"] = fmt.Sprint(state.Revision)
	if _, e := s.Execute(context.Background(), req); e == nil || e.Code != "claim_conflict" {
		t.Fatalf("stale run: %v", e)
	}
}
