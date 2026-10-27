package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
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
	if !reflect.DeepEqual(path, []string{"operations", "0", "depends_on"}) {
		t.Fatal(path)
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
