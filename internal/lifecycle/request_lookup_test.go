package lifecycle

import (
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
)

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
