package lifecycle

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestRecoverCompletedProjectWithoutDeclarations(t *testing.T) {
	for _, action := range []string{ActionUp, ActionRestart, ActionDown} {
		t.Run(action, func(t *testing.T) {
			p := testProject(t, "web")
			canonical, err := location.Canonical(p.Root)
			if err != nil {
				t.Fatal(err)
			}
			p.Root = canonical
			old := runningRecord(p, "web", tasks.ID())
			f := &fakeProcesses{records: []services.Record{old}}
			f.apply = func(q services.Request, _ int) (services.Result, *protocol.Error) {
				r := runningRecord(p, "web", q.RequestID)
				if q.Action == "stop" {
					r.ID = q.ID
					now := time.Now().UTC()
					r.EndedAt = &now
					r.State = "stopped"
				}
				return services.Result{Item: r, Changed: true}, nil
			}
			m := Manager{Data: t.TempDir(), Processes: f}
			q := Request{Action: action, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}
			first, e := m.Apply(context.Background(), q)
			if e != nil {
				t.Fatal(e)
			}
			before, _ := os.ReadFile(m.receiptPath(q.RequestID))
			q.Project = project.Context{}
			out, found, e := m.RecoverRequest(context.Background(), q, p.Root, network.PrepareAuto)
			if e != nil || !found || !out.Replayed || out.Items[0].Item.ID != first.Items[0].Item.ID || len(f.calls) != 1 {
				t.Fatal(out, found, e, f.calls)
			}
			after, _ := os.ReadFile(m.receiptPath(q.RequestID))
			if string(before) != string(after) {
				t.Fatal("completed receipt changed")
			}
			q.Commands = []string{"other"}
			if _, found, e := m.RecoverRequest(context.Background(), q, p.Root, network.PrepareAuto); !found || e == nil || e.Code != "request_conflict" {
				t.Fatal(found, e)
			}
		})
	}
}

func TestRecoverProjectReadinessUsesFrozenExecution(t *testing.T) {
	p := testProject(t, "web")
	canonical, err := location.Canonical(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	p.Root = canonical
	f := &fakeProcesses{}
	f.apply = func(q services.Request, _ int) (services.Result, *protocol.Error) {
		r := runningRecord(p, q.Command, q.RequestID)
		r.InputSnapshotID = strings.Repeat("a", 32)
		r.ReadyConfigured = true
		return services.Result{Item: r, Changed: true}, nil
	}
	f.wait = func(id string, n int) (services.Record, *protocol.Error) {
		r := runningRecord(p, "web", id)
		r.ReadyConfigured = true
		if n == 1 {
			return r, protocol.NewError("readiness_timeout", "Not ready yet.", 3, nil)
		}
		return r, nil
	}
	m := Manager{Data: t.TempDir(), Processes: f}
	q := Request{Action: ActionUp, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}
	if _, e := m.Apply(context.Background(), q); e == nil {
		t.Fatal("missing pending outcome")
	}
	q.Project = project.Context{}
	out, found, e := m.RecoverRequest(context.Background(), q, p.Root, network.PrepareAuto)
	if e != nil || !found || !out.Replayed || out.Items[0].Status != "ready" || f.calls[0].RequestID != f.calls[1].RequestID || len(f.waitCalls) != 2 {
		t.Fatal(out, found, e)
	}
}

func TestRecoveryCannotInitializeProjectRequest(t *testing.T) {
	p := testProject(t, "web")
	f := &fakeProcesses{}
	m := Manager{Data: t.TempDir(), Processes: f}
	q := Request{RecoverOnly: true, Action: ActionUp, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}
	if _, e := m.Apply(context.Background(), q); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
	if len(f.calls) != 0 {
		t.Fatal("recovery admitted new request")
	}
}
