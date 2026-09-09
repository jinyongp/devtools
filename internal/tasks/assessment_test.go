package tasks

import (
	"fmt"
	"testing"
)

func applyTestEvent(s *State, action, id string, data Object) {
	s.Apply(Event{ID: ID(), RequestID: ID(), Sequence: s.Revision + 1, At: stamp(), Action: action, Target: id, Data: data})
}

func assessmentFixture(t *testing.T) (*State, string, string, string) {
	t.Helper()
	s := NewState()
	applyTestEvent(s, "profile.upgraded", "", s.upgradeEvent().Data)
	w, a, b := ID(), ID(), ID()
	applyTestEvent(s, "workstream.create", w, Object{"title": "Work"})
	applyTestEvent(s, "spec.set", w, Object{"body": "common", "requirements": []Object{{"key": "R1", "text": "One"}, {"key": "R2", "text": "Two"}},
		"acceptance": []Object{{"key": "A1", "text": "One", "requirement_keys": []string{"R1"}}, {"key": "A2", "text": "Two", "requirement_keys": []string{"R2"}}}})
	for n, id := range []string{a, b} {
		applyTestEvent(s, "task.add", id, Object{"title": "Task", "workstream_id": w, "acceptance_keys": []string{fmt.Sprintf("A%d", n+1)}})
		applyTestEvent(s, "validation.add", ID(), Object{"title": "Check", "method": "test", "task_id": id})
	}
	applyTestEvent(s, "plan.set", w, Object{"body": "plan"})
	applyTestEvent(s, "workstream.activate", w, Object{})
	return s, w, a, b
}

func finishTestTask(s *State, id string) {
	run := ID()
	applyTestEvent(s, "run.claimed", id, Object{"run_id": run, "directory": "/tmp"})
	for _, v := range s.List("validation") {
		if s.owner(v).ID == id {
			basis := ID()
			applyTestEvent(s, "validation.basis", v.ID, Object{"basis_id": basis, "definition_version": 2, "fingerprint": s.basisFingerprint(v), "code": []any{}, "run_id": run})
			applyTestEvent(s, "validation.record", v.ID, Object{"basis_id": basis, "record_id": ID(), "result": "pass", "run_id": run})
		}
	}
	applyTestEvent(s, "task.completed", id, Object{"run_id": run, "summary": "complete"})
}

func TestAssessmentPreservesUnrelatedCompletion(t *testing.T) {
	s, w, a, b := assessmentFixture(t)
	finishTestTask(s, a)
	initial := s.Assessment(a)
	if initial.CompletionStatus != "current" {
		t.Fatal(initial)
	}
	applyTestEvent(s, "task.update", a, Object{"title": "Renamed"})
	if got := s.Assessment(a); got.Signature != initial.Signature || got.CompletionStatus != "current" {
		t.Fatal("title invalidates evidence", got)
	}
	before := s.clone()
	applyTestEvent(s, "task.update", b, Object{"description": "Changed other work"})
	if got := s.Assessment(a); got.Signature != initial.Signature || got.CompletionStatus != "current" {
		t.Fatal("unrelated change invalidates evidence", got)
	}
	if contains(DefinitionChanges(before, s), a) {
		t.Fatal("unrelated task in impact")
	}
	spec := objectValue(s.Items[w].Props["spec"])
	updated := s.clone()
	newSpec := objectValue(updated.Items[w].Props["spec"])
	entries := objects(newSpec, "acceptance")
	entries[1]["text"] = "new second criterion"
	newSpec["acceptance"] = entries
	applyTestEvent(s, "spec.set", w, newSpec)
	if s.Assessment(a).CompletionStatus != "current" {
		t.Fatal("other criterion invalidates first task")
	}
	spec = objectValue(s.Items[w].Props["spec"])
	next := s.clone()
	newSpec = objectValue(next.Items[w].Props["spec"])
	newSpec["body"] = "changed common body"
	applyTestEvent(s, "spec.set", w, newSpec)
	if s.Assessment(a).CompletionStatus != "stale" {
		t.Fatal("common change not propagated", spec)
	}
}

func TestAssessmentPropagatesPrerequisiteAndWorkstreamChanges(t *testing.T) {
	s, w, a, b := assessmentFixture(t)
	applyTestEvent(s, "task.depends", b, Object{"depends_on": []string{a}})
	if s.CanClaim(b) {
		t.Fatal("unfinished prerequisite accepted")
	}
	finishTestTask(s, a)
	finishTestTask(s, b)
	applyTestEvent(s, "workstream.close", w, Object{})
	x, y := ID(), ID()
	applyTestEvent(s, "workstream.create", x, Object{"title": "Next"})
	applyTestEvent(s, "workstream.depends", x, Object{"depends_on": []string{w}})
	applyTestEvent(s, "spec.set", x, Object{"body": "next"})
	applyTestEvent(s, "plan.set", x, Object{"body": "next"})
	applyTestEvent(s, "workstream.activate", x, Object{})
	applyTestEvent(s, "task.add", y, Object{"title": "Follow", "workstream_id": x, "acceptance": []string{"works"}})
	applyTestEvent(s, "validation.add", ID(), Object{"title": "check", "method": "test", "task_id": y})
	finishTestTask(s, y)
	old := s.clone()
	applyTestEvent(s, "task.update", a, Object{"description": "New behavior"})
	for _, id := range []string{a, b, w, y} {
		if s.Assessment(id).CompletionStatus != "stale" {
			t.Fatalf("%s not stale: %+v", id, s.Assessment(id))
		}
	}
	for _, id := range []string{a, b, w, x, y} {
		if !contains(DefinitionChanges(old, s), id) {
			t.Fatal("missing transitive impact", id)
		}
	}
}

func TestAssessmentDraftAndUncoveredTask(t *testing.T) {
	s, w, a, b := assessmentFixture(t)
	if !s.CanClaim(a) || !s.CanClaim(b) {
		t.Fatal("ready tasks blocked")
	}
	applyTestEvent(s, "task.add", ID(), Object{"title": "Unfinished plan entry", "workstream_id": w})
	if !s.CanClaim(a) {
		t.Fatal("unrelated coverage blocks task")
	}
	applyTestEvent(s, "workstream.close", w, Object{})
	applyTestEvent(s, "workstream.reopen", w, Object{})
	if s.CanClaim(a) {
		t.Fatal("explicit draft ignored")
	}
}

func TestAssessmentNoCompletionFingerprintCycle(t *testing.T) {
	s, w, a, b := assessmentFixture(t)
	before := s.Assessment(a).Signature
	finishTestTask(s, a)
	if s.Assessment(a).Signature != before {
		t.Fatal("task completion invalidates its signature")
	}
	finishTestTask(s, b)
	before = s.Assessment(w).Signature
	applyTestEvent(s, "workstream.close", w, Object{})
	if s.Assessment(w).Signature != before || s.Assessment(w).CompletionStatus != "current" {
		t.Fatal("close signature cycle")
	}
}

func TestAssessmentLargeDAGIsIterative(t *testing.T) {
	s := NewState()
	s.Version = 2
	previous := ""
	for n := 0; n < 1000; n++ {
		id := fmt.Sprintf("task-%04d", n)
		deps := []string{}
		if previous != "" {
			deps = append(deps, previous)
		}
		s.Items[id] = &Item{ID: id, Kind: "task", State: "open", Props: Object{}, Depends: deps, Order: n}
		s.Tracking[id] = newDefinitionBasis(n + 1)
		previous = id
	}
	all := s.Assessments()
	if len(all) != 1000 {
		t.Fatal(len(all))
	}
	if !all["task-0000"].Ready || all["task-0999"].Ready {
		t.Fatal("DAG readiness incorrect")
	}
}
