package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProxyBatchLaunchesPeersBeforeReadinessAndKeepsPreparedSnapshot(t *testing.T) {
	p := testProject(t, "web", "api")
	f := &fakeProcesses{}
	prepared := 0
	f.apply = func(q services.Request, _ int) (services.Result, *protocol.Error) {
		if prepared != 1 || !q.ProxyMode || q.Network == nil {
			t.Fatal("child started without complete preparation")
		}
		r := runningRecord(p, q.Command, q.RequestID)
		r.ProxyMode, r.ReadyConfigured = true, true
		return services.Result{Item: r, Changed: true}, nil
	}
	f.wait = func(id string, _ int) (services.Record, *protocol.Error) {
		if len(f.calls) != 2 {
			t.Fatal("web readiness waited before api launched")
		}
		r := runningRecord(p, "web", id)
		return r, nil
	}
	m := Manager{Data: t.TempDir(), Processes: f, PrepareNetwork: func(context.Context, Request, []services.Record, string) (map[string]*network.ExecutionSnapshot, *protocol.Error) {
		prepared++
		return map[string]*network.ExecutionSnapshot{"web": {ID: tasks.ID()}, "api": {ID: tasks.ID()}}, nil
	}}
	q := Request{Action: ActionUp, Project: p, Commands: []string{"web", "api"}, ProxyMode: true, Timeout: time.Second, RequestID: tasks.ID()}
	if _, e := m.Apply(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Apply(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	if prepared != 1 || len(f.calls) != 2 || len(f.waitCalls) != 2 {
		t.Fatalf("replay repeated work: prepare=%d calls=%d waits=%d", prepared, len(f.calls), len(f.waitCalls))
	}
	q.ProxyMode = false
	if _, e := m.Apply(context.Background(), q); e == nil || e.Code != "request_conflict" {
		t.Fatalf("mode changed on replay: %v", e)
	}
}

func TestProxyRestartPreparationBarrierMixedModesAndRetry(t *testing.T) {
	p := testProject(t, "web", "api", "legacy")
	web, api, legacy := runningRecord(p, "web", tasks.ID()), runningRecord(p, "api", tasks.ID()), runningRecord(p, "legacy", tasks.ID())
	web.ProxyMode, api.ProxyMode = true, true
	f := &fakeProcesses{records: []services.Record{web, api, legacy}}
	blocked := true
	m := Manager{Data: t.TempDir(), Processes: f, PrepareNetwork: func(context.Context, Request, []services.Record, string) (map[string]*network.ExecutionSnapshot, *protocol.Error) {
		if blocked {
			return nil, protocol.NewError("permission_required", "DNS preparation requires authorization.", 3, nil)
		}
		return map[string]*network.ExecutionSnapshot{"web": {ID: tasks.ID()}, "api": {ID: tasks.ID()}}, nil
	}, Validate: func(context.Context, project.Context, string, *string, *string) *protocol.Error { return nil }}
	q := Request{Action: ActionRestart, Project: p, Commands: []string{"web", "api", "legacy"}, Timeout: time.Second, RequestID: tasks.ID()}
	if _, e := m.Apply(context.Background(), q); e == nil {
		t.Fatal("preparation succeeded unexpectedly")
	}
	if len(f.calls) != 0 {
		t.Fatal("original execution stopped before preparation succeeded")
	}
	blocked = false
	apiAttempts := 0
	f.apply = func(r services.Request, _ int) (services.Result, *protocol.Error) {
		command := "legacy"
		if r.ID == web.ID {
			command = "web"
		}
		if r.ID == api.ID {
			command = "api"
			apiAttempts++
			if apiAttempts == 1 {
				return services.Result{}, protocol.NewError("execution_failed", "api failed", 126, nil)
			}
		}
		if r.ProxyMode != (command != "legacy") {
			t.Fatalf("mixed mode lost: %s", command)
		}
		record := runningRecord(p, command, r.RequestID)
		record.Previous = r.ID
		record.ReadyConfigured = true
		return services.Result{Item: record, Changed: true}, nil
	}
	f.wait = func(id string, _ int) (services.Record, *protocol.Error) {
		if len(f.calls) < 3 {
			t.Fatal("wait ran before every replacement launch")
		}
		return runningRecord(p, "web", id), nil
	}
	if _, e := m.Apply(context.Background(), q); e == nil {
		t.Fatal("partial failure lost")
	}
	if _, e := m.Apply(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	if len(f.calls) != 4 || f.calls[3].ID != api.ID {
		t.Fatalf("successful sibling repeated: %#v", f.calls)
	}
}
