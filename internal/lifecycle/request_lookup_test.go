package lifecycle

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestInspectProjectRequestNeverResumesChildren(t *testing.T) {
	f := &fakeProcesses{}
	m := Manager{Data: t.TempDir(), Processes: f}
	id := tasks.ID()
	if _, e := m.InspectRequest(id); e == nil || e.Code != "request_not_found" {
		t.Fatal(e)
	}
	if _, e := os.Stat(m.base()); !os.IsNotExist(e) {
		t.Fatal("query initialized store", e)
	}
	for _, done := range []bool{false, true} {
		v := receipt{Fingerprint: strings.Repeat("b", 64), Action: ActionUp, Profile: "app", Directory: t.TempDir(), Items: []receiptItem{{Command: "web", RequestID: tasks.ID(), Done: done, Result: Item{Command: "web", Status: "failed", Condition: &Condition{Code: "execution_failed", Message: "failed"}}}}}
		if e := writeReceipt(m.receiptPath(id), v); e != nil {
			t.Fatal(e)
		}
		before, _ := os.ReadFile(m.receiptPath(id))
		got, e := m.InspectRequest(id)
		if e != nil || (got.State == "completed") != done || got.Result.Items[0].Condition.Code != "execution_failed" {
			t.Fatal(got, e)
		}
		after, _ := os.ReadFile(m.receiptPath(id))
		if !bytes.Equal(before, after) || len(f.calls) != 0 || len(f.waitCalls) != 0 {
			t.Fatal("query resumed or rewrote operation")
		}
	}
}

func TestInspectProjectRequestPublishesOnlySafeConditionDetails(t *testing.T) {
	m := Manager{Data: t.TempDir()}
	id := tasks.ID()
	v := receipt{Fingerprint: strings.Repeat("a", 64), Action: ActionUp, Profile: "app", Directory: t.TempDir(), Items: []receiptItem{{Command: "web", RequestID: tasks.ID(), Done: true, Result: Item{Command: "web", Status: "failed", Condition: &Condition{Code: "execution_failed", Message: "Child did not start.", Details: map[string]any{
		"fingerprint": "private-query-canary", "supervisor_token": "private-query-canary", "facts": map[string]any{"state": "failed", "private_marker": "private-query-canary"},
	}}}}}}
	if e := writeReceipt(m.receiptPath(id), v); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(m.receiptPath(id))
	out, e := m.InspectRequest(id)
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(out)
	if bytes.Contains(encoded, []byte("private-query-canary")) {
		t.Fatal("raw private error details reached the public request query")
	}
	condition := out.Result.Items[0].Condition
	if condition.Code != "execution_failed" || condition.Message != "Child did not start." || condition.Details["facts"].(map[string]any)["state"] != "failed" {
		t.Fatal("lost public failure facts")
	}
	after, _ := os.ReadFile(m.receiptPath(id))
	if !bytes.Equal(before, after) {
		t.Fatal("sanitization rewrote saved history")
	}
}

func TestLegacyLifecycleFingerprintAndLookup(t *testing.T) {
	m := Manager{Data: t.TempDir(), Processes: &fakeProcesses{}}
	q := Request{Action: ActionUp, Project: project.Context{Profile: "app", Root: "/project", Commands: map[string]project.Command{"web": {Exec: []string{"legacy"}}}}, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}
	const legacy = "2fb6321dc77ac5acc142ba56866bab70705c60815f54470730d15f1326a1c824"
	if got := fingerprint(q); got != legacy {
		t.Fatalf("legacy projection changed: %s", got)
	}
	if found, e := m.HasRequest(q.RequestID); e != nil || found {
		t.Fatal(found, e)
	}
	if e := writeReceipt(m.receiptPath(q.RequestID), receipt{Fingerprint: legacy, Action: ActionUp, Profile: "app", Directory: "/project", Items: []receiptItem{}}); e != nil {
		t.Fatal(e)
	}
	if found, e := m.HasRequest(q.RequestID); e != nil || !found {
		t.Fatal(found, e)
	}
}
