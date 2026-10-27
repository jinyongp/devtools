package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestTaskFinalReferenceFailureUsesOriginatingOperation(t *testing.T) {
	s, w, task, _ := currentFixture(t)
	state, e := s.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Execute(context.Background(), Request{Action: "workstream.edited", Target: w, Body: editBody(
		Object{"op": "task.depends.set", "id": task, "depends_on": []string{"00000000-0000-4000-8000-000000000001"}},
		Object{"op": "workstream.update", "value": Object{"title": "Later operation"}},
	), Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}})
	if e == nil || e.Details["operation_index"] != 0 {
		t.Fatal(e)
	}
	path := e.Details["sources"].([]any)[0].(map[string]any)["path"]
	if !reflect.DeepEqual(path, []string{"operations", "0", "depends_on", "0"}) {
		t.Fatal(path)
	}
}

func TestEditReferenceProvenance(t *testing.T) {
	const missing = "00000000-0000-4000-8000-000000000001"
	for _, anchor := range []string{"before_id", "after_id"} {
		for _, removedFirst := range []bool{false, true} {
			t.Run(fmt.Sprint(anchor, removedFirst), func(t *testing.T) {
				s, w, a, b := assessmentFixture(t)
				move := Object{"op": "task.move", "id": a, anchor: b}
				remove := Object{"op": "task.remove", "id": b}
				ops, index := []Object{move, remove}, "0"
				if removedFirst {
					ops, index = []Object{remove, move}, "1"
				}
				before := hash(s)
				_, e := EvaluateEdit(s, w, editBody(ops...), nil)
				assertEditReference(t, e, []string{"operations", index, anchor}, b)
				if hash(s) != before {
					t.Fatal("failed edit changed state")
				}
				_, e = EvaluateEdit(s, w, editBody(Object{"op": "task.move", "id": a, anchor: missing}), nil)
				assertEditReference(t, e, []string{"operations", "0", anchor}, missing)
			})
		}
	}
	s, w, a, b := assessmentFixture(t)
	_, e := EvaluateEdit(s, w, editBody(Object{"op": "task.move", "id": a, "after_id": b}, Object{"op": "task.remove", "id": a}), nil)
	assertEditReference(t, e, []string{"operations", "0", "id"}, a)
	_, e = EvaluateEdit(s, w, editBody(Object{"op": "task.move", "id": a, "after_id": b}, Object{"op": "task.move", "id": a, "before_id": b}, Object{"op": "task.remove", "id": b}), nil)
	assertEditReference(t, e, []string{"operations", "1", "before_id"}, b)
	_, e = EvaluateEdit(s, w, editBody(Object{"op": "workstream.depends.set", "depends_on": []string{missing}}), nil)
	assertEditReference(t, e, []string{"operations", "0", "depends_on", "0"}, missing)
	_, e = EvaluateEdit(s, w, editBody(Object{"op": "acceptance.update", "key": "A1", "value": Object{"requirement_keys": []string{"R1", "R1", "MISSING"}}}), nil)
	assertEditReference(t, e, []string{"operations", "0", "value", "requirement_keys", "2"}, "")
	foreign, foreignTask := ID(), ID()
	applyTestEvent(s, "workstream.create", foreign, Object{"title": "Other"})
	applyTestEvent(s, "task.add", foreignTask, Object{"title": "Other task", "workstream_id": foreign})
	for _, target := range []string{missing, w, foreignTask} {
		_, e := EvaluateEdit(s, w, editBody(Object{"op": "task.depends.set", "id": a, "depends_on": []string{b, b, target}}), nil)
		assertEditReference(t, e, []string{"operations", "0", "depends_on", "2"}, target)
	}
	for _, row := range []struct{ field, target string }{
		{"task_id", missing}, {"task_id", w}, {"task_id", foreignTask},
		{"workstream_id", missing}, {"workstream_id", a}, {"workstream_id", foreign},
	} {
		_, e := EvaluateEdit(s, w, editBody(Object{"op": "validation.add", "value": Object{"title": "Check", "method": "Test", row.field: row.target}}), nil)
		assertEditReference(t, e, []string{"operations", "0", "value", row.field}, row.target)
	}
	// A request-local anchor is resolved for commit diagnostics, but preview IDs
	// must never masquerade as public UUID facts.
	for _, allocations := range []map[int]string{nil, {0: ID()}} {
		_, e := EvaluateEdit(s, w, editBody(
			Object{"op": "validation.add", "ref": "wrong_kind", "value": Object{"title": "Check", "method": "Test", "task_id": a}},
			Object{"op": "task.move", "id": b, "after_id": "@wrong_kind"},
		), allocations)
		want := ""
		if allocations != nil {
			want = allocations[0]
		}
		assertEditReference(t, e, []string{"operations", "1", "after_id"}, want)
	}
	// Restoration and repeated moves retain their normal successful semantics.
	removed, e := EvaluateEdit(s, w, editBody(Object{"op": "task.remove", "id": b}), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = EvaluateEdit(removed.Candidate, w, editBody(Object{"op": "task.restore", "id": b}, Object{"op": "task.move", "id": a, "after_id": b}, Object{"op": "task.move", "id": a, "before_id": b}), nil); e != nil {
		t.Fatal(e)
	}
}

func assertEditReference(t *testing.T, pe *protocol.Error, path []string, target string) {
	t.Helper()
	if pe == nil {
		t.Fatal("invalid reference accepted")
	}
	got := pe.Details["sources"].([]any)[0].(map[string]any)["path"]
	if !reflect.DeepEqual(got, path) || pe.Details["reference_id"] != target && !(target == "" && pe.Details["reference_id"] == nil) {
		t.Fatal(pe.Details, "want", path, target)
	}
}

func TestTaskCloseCoverageFailureKeepsSafeChecks(t *testing.T) {
	s, w, _, v := currentFixture(t)
	call(t, s, "workstream.edited", w, editBody(Object{"op": "validation.remove", "id": v}))
	state, e := s.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Execute(context.Background(), Request{Action: "workstream.close", Target: w, Body: Object{}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}})
	if e == nil || e.Code != "validation_required" || len(e.Details["checks"].([]any)) == 0 {
		t.Fatal(e)
	}
	after, err := s.Read(context.Background())
	if err != nil || after.Revision != state.Revision {
		t.Fatal("failed close changed revision", err)
	}
}

func TestTaskUnknownReferenceDoesNotEchoCredentialShapedInput(t *testing.T) {
	s, w, _, _ := currentFixture(t)
	state, e := s.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Execute(context.Background(), Request{Action: "workstream.edited", Target: w, Body: editBody(Object{"op": "validation.add", "value": Object{"title": "Fixture", "method": "test", "task_id": "@PRIVATE-CONTEXT-CANARY"}}), Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)}})
	if e == nil {
		t.Fatal("unknown reference accepted")
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "PRIVATE-") {
		t.Fatal("input escaped diagnostic")
	}
}
