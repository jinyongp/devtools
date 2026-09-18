package lifecycle

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestUpSkipsColdStartPreflightForReusableSingletons(t *testing.T) {
	p := testProject(t, "web", "api")
	web := runningRecord(p, "web", tasks.ID())
	other := p
	other.Root = t.TempDir()
	otherAPI := runningRecord(other, "api", tasks.ID())
	processes := &fakeProcesses{records: []services.Record{web, otherAPI}}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		if request.Command == "web" {
			return services.Result{Item: web}, nil
		}
		return services.Result{Item: runningRecord(p, request.Command, request.RequestID), Changed: true}, nil
	}
	checked := []string{}
	manager := Manager{Data: t.TempDir(), Processes: processes, Preflight: func(_ context.Context, _ project.Context, name string, _ *string) *protocol.Error {
		checked = append(checked, name)
		if name == "web" {
			return protocol.NewError("port_run_active", "The running server already owns its port.", 3, nil)
		}
		return nil
	}}
	result, err := manager.Apply(context.Background(), Request{Action: ActionUp, Project: p, Commands: []string{"web", "api"}, Timeout: time.Second, RequestID: tasks.ID()})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(checked, []string{"api"}) {
		t.Fatalf("wrong cold-start preflight selection: %v", checked)
	}
	if result.Items[0].Changed || result.Items[0].Item.ID != web.ID || !result.Items[1].Changed {
		t.Fatalf("singleton reuse failed: %#v", result)
	}
}

func TestUpStillPreflightsNewCommandsBeforeAnyApply(t *testing.T) {
	p := testProject(t, "web", "api")
	processes := &fakeProcesses{records: []services.Record{runningRecord(p, "web", tasks.ID())}}
	manager := Manager{Data: t.TempDir(), Processes: processes, Preflight: func(_ context.Context, _ project.Context, name string, _ *string) *protocol.Error {
		if name != "api" {
			t.Fatalf("unexpected cold-start preflight: %s", name)
		}
		return protocol.NewError("port_in_use", "The new command cannot acquire its port.", 3, nil)
	}}
	_, err := manager.Apply(context.Background(), Request{Action: ActionUp, Project: p, Commands: []string{"web", "api"}, Timeout: time.Second, RequestID: tasks.ID()})
	if err == nil || err.Code != "port_in_use" || len(processes.calls) != 0 {
		t.Fatalf("new-command preflight was bypassed: %v; calls=%d", err, len(processes.calls))
	}
}

func TestUpRechecksPreflightIfReuseCandidateBecomesActualStart(t *testing.T) {
	p := testProject(t, "web")
	web := runningRecord(p, "web", tasks.ID())
	processes := &fakeProcesses{records: []services.Record{web}}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		if request.BeforeStart == nil {
			t.Fatal("reuse candidate lost fallback preflight")
		}
		if err := request.BeforeStart(context.Background()); err != nil {
			return services.Result{}, err
		}
		return services.Result{Item: runningRecord(p, request.Command, request.RequestID), Changed: true}, nil
	}
	checked := []string{}
	manager := Manager{Data: t.TempDir(), Processes: processes, Preflight: func(_ context.Context, _ project.Context, name string, _ *string) *protocol.Error {
		checked = append(checked, name)
		return nil
	}}
	result, err := manager.Apply(context.Background(), Request{Action: ActionUp, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()})
	if err != nil || !result.Changed || !reflect.DeepEqual(checked, []string{"web"}) {
		t.Fatalf("raced cold start was not preflighted: result=%#v checked=%v err=%v", result, checked, err)
	}
}

func TestUpRechecksColdStartImmediatelyBeforeApply(t *testing.T) {
	p := testProject(t, "web")
	processes := &fakeProcesses{}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		if request.BeforeStart == nil {
			t.Fatal("cold start lost immediate preflight")
		}
		if err := request.BeforeStart(context.Background()); err != nil {
			return services.Result{}, err
		}
		return services.Result{Item: runningRecord(p, request.Command, request.RequestID), Changed: true}, nil
	}
	checked := 0
	manager := Manager{
		Data:      t.TempDir(),
		Processes: processes,
		Preflight: func(context.Context, project.Context, string, *string) *protocol.Error {
			checked++
			return nil
		},
	}
	result, err := manager.Apply(context.Background(), Request{Action: ActionUp, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()})
	if err != nil || !result.Changed || checked != 2 {
		t.Fatalf("cold start was not rechecked at apply: result=%#v checked=%d err=%v", result, checked, err)
	}
}
