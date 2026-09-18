package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

type fakeProcesses struct {
	mu        sync.Mutex
	records   []services.Record
	calls     []services.Request
	waitCalls []string
	apply     func(services.Request, int) (services.Result, *protocol.Error)
	wait      func(string, int) (services.Record, *protocol.Error)
}

func (f *fakeProcesses) Apply(_ context.Context, request services.Request) (services.Result, *protocol.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request)
	count := 0
	for _, call := range f.calls {
		if call.Action == request.Action && call.Command == request.Command {
			count++
		}
	}
	if f.apply != nil {
		return f.apply(request, count)
	}
	return services.Result{}, nil
}

func (f *fakeProcesses) List(_ context.Context, _ string) ([]services.Record, *protocol.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.Record{}, f.records...), nil
}

func (f *fakeProcesses) Wait(_ context.Context, id string, _ time.Duration) (services.Record, *protocol.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waitCalls = append(f.waitCalls, id)
	if f.wait != nil {
		return f.wait(id, len(f.waitCalls))
	}
	return services.Record{}, nil
}

func (f *fakeProcesses) callCount(action, command string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if call.Action == action && call.Command == command {
			count++
		}
	}
	return count
}

func (f *fakeProcesses) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if call.Action == "stop" {
			count++
		}
	}
	return count
}

func testProject(t *testing.T, commands ...string) project.Context {
	t.Helper()
	defs := map[string]project.Command{}
	for _, name := range commands {
		defs[name] = project.Command{Exec: []string{"true"}}
	}
	return project.Context{Profile: "app", Root: t.TempDir(), Commands: defs}
}

func runningRecord(p project.Context, command, id string) services.Record {
	now := time.Now().UTC()
	return services.Record{ID: id, Profile: p.Profile, Instance: "instance", Directory: p.Root, Command: command, CreatedAt: now, StartedAt: &now, State: "running"}
}

func TestUpPreflightsAllCommandsBeforeMutation(t *testing.T) {
	p := testProject(t, "web", "api")
	processes := &fakeProcesses{}
	manager := Manager{
		Data:      t.TempDir(),
		Processes: processes,
		Preflight: func(_ context.Context, _ project.Context, command string, _ *string) *protocol.Error {
			if command == "api" {
				return protocol.NewError("requirements_failed", "API requirements are missing.", 3, nil)
			}
			return nil
		},
	}
	request := Request{Action: ActionUp, Project: p, Commands: []string{"web", "api"}, Timeout: time.Second, RequestID: tasks.ID()}
	if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "requirements_failed" {
		t.Fatalf("expected preflight failure, got %v", err)
	}
	if len(processes.calls) != 0 {
		t.Fatalf("process mutation occurred before preflight completed: %#v", processes.calls)
	}

	manager.Preflight = nil
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		record := runningRecord(p, request.Command, request.RequestID)
		return services.Result{Item: record, Changed: true}, nil
	}
	result, err := manager.Apply(context.Background(), request)
	if err != nil || len(result.Items) != 2 || result.Replayed {
		t.Fatalf("request did not remain reusable after preflight failure: %#v %v", result, err)
	}
}

func TestUpPartialFailureRetriesOnlyIncompleteChildren(t *testing.T) {
	p := testProject(t, "web", "api")
	processes := &fakeProcesses{}
	processes.apply = func(request services.Request, count int) (services.Result, *protocol.Error) {
		if request.Command == "api" && count == 1 {
			return services.Result{}, protocol.NewError("execution_failed", "API failed to start.", 126, nil)
		}
		record := runningRecord(p, request.Command, request.RequestID)
		return services.Result{Item: record, Changed: true}, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionUp, Project: p, Commands: []string{"web", "api"}, Timeout: time.Second, RequestID: tasks.ID()}

	first, err := manager.Apply(context.Background(), request)
	if err == nil || err.Code != "project_operation_failed" || first.Replayed || !first.Changed {
		t.Fatalf("unexpected partial result: %#v %v", first, err)
	}
	if first.Items[0].Command != "web" || first.Items[0].Status != "running" || first.Items[1].Command != "api" || first.Items[1].Status != "failed" {
		t.Fatalf("unexpected child statuses: %#v", first.Items)
	}
	if processes.callCount("start", "web") != 1 || processes.callCount("start", "api") != 1 || processes.stopCount() != 0 {
		t.Fatalf("unexpected calls after partial failure: %#v", processes.calls)
	}
	processes.mu.Lock()
	apiRequestID := ""
	for _, call := range processes.calls {
		if call.Action == "start" && call.Command == "api" {
			apiRequestID = call.RequestID
		}
	}
	processes.mu.Unlock()
	if apiRequestID == "" {
		t.Fatal("missing child request ID")
	}

	second, err := manager.Apply(context.Background(), request)
	if err != nil || !second.Replayed || !second.Changed || second.Items[0].Status != "running" || second.Items[1].Status != "running" {
		t.Fatalf("retry did not converge: %#v %v", second, err)
	}
	if processes.callCount("start", "web") != 1 || processes.callCount("start", "api") != 2 || processes.stopCount() != 0 {
		t.Fatalf("completed child was repeated or rollback occurred: %#v", processes.calls)
	}
	processes.mu.Lock()
	apiIDs := []string{}
	for _, call := range processes.calls {
		if call.Action == "start" && call.Command == "api" {
			apiIDs = append(apiIDs, call.RequestID)
		}
	}
	processes.mu.Unlock()
	if len(apiIDs) != 2 || apiIDs[0] != apiRequestID || apiIDs[1] == apiRequestID {
		t.Fatalf("terminal failure did not persist a fresh retry identity: %#v", apiIDs)
	}

	changed := request
	changed.Commands = []string{"api", "web"}
	if _, err := manager.Apply(context.Background(), changed); err == nil || err.Code != "request_conflict" {
		t.Fatalf("changed input reused request ID: %v", err)
	}
}

func TestUpPersistsPartialProcessResultAcrossPendingRetry(t *testing.T) {
	p := testProject(t, "web")
	processes := &fakeProcesses{}
	processes.apply = func(request services.Request, count int) (services.Result, *protocol.Error) {
		record := runningRecord(p, request.Command, request.RequestID)
		if count == 1 {
			record.State = "starting"
			return services.Result{Item: record, Changed: true}, protocol.NewError("process_pending", "Process start is still pending.", 3, nil)
		}
		return services.Result{Item: record}, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionUp, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}

	first, err := manager.Apply(context.Background(), request)
	if err == nil || err.Code != "project_operation_failed" || !first.Changed || first.Items[0].Status != "pending" || first.Items[0].Item == nil {
		t.Fatalf("partial process result was not persisted: %#v %v", first, err)
	}
	executionID := first.Items[0].Item.ID
	second, err := manager.Apply(context.Background(), request)
	if err != nil || !second.Replayed || !second.Changed || second.Items[0].Status != "running" || second.Items[0].Item == nil || second.Items[0].Item.ID != executionID {
		t.Fatalf("pending retry lost original mutation/result: %#v %v", second, err)
	}
}

func TestConcurrentUpConvergesToApplyAndReplayWithReadiness(t *testing.T) {
	p := testProject(t, "web")
	command := p.Commands["web"]
	command.Ready = &project.ReadyProbe{Exec: []string{"true"}}
	p.Commands["web"] = command
	processes := &fakeProcesses{}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		time.Sleep(10 * time.Millisecond)
		record := runningRecord(p, request.Command, request.RequestID)
		record.ReadyConfigured = true
		return services.Result{Item: record, Changed: true}, nil
	}
	processes.wait = func(id string, _ int) (services.Record, *protocol.Error) {
		record := runningRecord(p, "web", id)
		record.ReadyConfigured = true
		record.Readiness = &services.Readiness{Ready: true, CheckedAt: time.Now().UTC(), Reason: "probe_passed"}
		return record, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionUp, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}

	type outcome struct {
		result Result
		err    *protocol.Error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			result, err := manager.Apply(context.Background(), request)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	first, second := <-outcomes, <-outcomes
	if first.err != nil || second.err != nil || first.result.Replayed == second.result.Replayed {
		t.Fatalf("concurrent request did not converge to apply/replay: %#v %#v", first, second)
	}
	if first.result.Items[0].Status != "ready" || second.result.Items[0].Status != "ready" {
		t.Fatalf("readiness result was not persisted: %#v %#v", first.result, second.result)
	}
	if processes.callCount("start", "web") != 1 || len(processes.waitCalls) != 1 {
		t.Fatalf("concurrent request duplicated child work: calls=%#v waits=%#v", processes.calls, processes.waitCalls)
	}
}

func TestDownPersistsPartialProcessResultAcrossPendingRetry(t *testing.T) {
	p := testProject(t, "web")
	web := runningRecord(p, "web", tasks.ID())
	processes := &fakeProcesses{records: []services.Record{web}}
	processes.apply = func(request services.Request, count int) (services.Result, *protocol.Error) {
		if count == 1 {
			return services.Result{Item: web, Changed: true}, protocol.NewError("process_pending", "Process stop is still pending.", 3, nil)
		}
		stopped := web
		ended := time.Now().UTC()
		stopped.EndedAt = &ended
		stopped.State = "stopped"
		return services.Result{Item: stopped}, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionDown, Project: p, Commands: []string{"web"}, RequestID: tasks.ID()}

	first, err := manager.Apply(context.Background(), request)
	if err == nil || err.Code != "project_operation_failed" || !first.Changed || first.Items[0].Status != "pending" || first.Items[0].Item == nil || first.Items[0].Item.ID != web.ID {
		t.Fatalf("partial stop result was not persisted: %#v %v", first, err)
	}
	second, err := manager.Apply(context.Background(), request)
	if err != nil || !second.Replayed || !second.Changed || second.Items[0].Status != "stopped" || second.Items[0].Item == nil || second.Items[0].Item.ID != web.ID {
		t.Fatalf("pending stop retry lost original mutation/result: %#v %v", second, err)
	}
}

func TestDownSnapshotsTargetsAndNoOpReplayMetadata(t *testing.T) {
	p := testProject(t, "web", "api")
	web := runningRecord(p, "web", tasks.ID())
	api := runningRecord(p, "api", tasks.ID())
	otherProject := runningRecord(p, "web", tasks.ID())
	otherProject.Directory = t.TempDir()
	ended := runningRecord(p, "old", tasks.ID())
	endedAt := time.Now().UTC()
	ended.EndedAt = &endedAt
	ended.State = "exited"
	processes := &fakeProcesses{records: []services.Record{web, api, otherProject, ended}}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		for _, record := range []services.Record{web, api} {
			if record.ID == request.ID {
				ended := time.Now().UTC()
				record.EndedAt = &ended
				record.State = "stopped"
				return services.Result{Item: record, Changed: true}, nil
			}
		}
		return services.Result{}, protocol.NewError("process_not_found", "missing", 3, nil)
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionDown, Project: p, RequestID: tasks.ID()}
	first, err := manager.Apply(context.Background(), request)
	if err != nil || first.Replayed || len(first.Items) != 2 || processes.stopCount() != 2 {
		t.Fatalf("down did not stop the initial project snapshot: %#v %v calls=%#v", first, err, processes.calls)
	}

	processes.mu.Lock()
	processes.records = append(processes.records, runningRecord(p, "worker", tasks.ID()))
	processes.mu.Unlock()
	second, err := manager.Apply(context.Background(), request)
	if err != nil || !second.Replayed || processes.stopCount() != 2 {
		t.Fatalf("down retry re-enumerated or repeated stops: %#v %v calls=%#v", second, err, processes.calls)
	}

	emptyManager := Manager{Data: t.TempDir(), Processes: &fakeProcesses{}}
	emptyRequest := Request{Action: ActionDown, Project: p, RequestID: tasks.ID()}
	noOp, err := emptyManager.Apply(context.Background(), emptyRequest)
	if err != nil || noOp.Replayed || noOp.Changed || len(noOp.Items) != 0 {
		t.Fatalf("first no-op down has wrong metadata: %#v %v", noOp, err)
	}
	replayed, err := emptyManager.Apply(context.Background(), emptyRequest)
	if err != nil || !replayed.Replayed || replayed.Changed || len(replayed.Items) != 0 {
		t.Fatalf("no-op down retry was not replayed: %#v %v", replayed, err)
	}
}

func TestRestartSnapshotsTargetsAndDoesNotStartMissingCommands(t *testing.T) {
	p := testProject(t, "web", "api")
	web := runningRecord(p, "web", tasks.ID())
	processes := &fakeProcesses{records: []services.Record{web}}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		if request.Action != "restart" || request.ID != web.ID {
			t.Fatalf("unexpected restart request: %#v", request)
		}
		record := runningRecord(p, "web", request.RequestID)
		record.Previous = web.ID
		return services.Result{Item: record, Changed: true}, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionRestart, Project: p, Commands: []string{"web", "api"}, Timeout: time.Second, RequestID: tasks.ID()}

	first, err := manager.Apply(context.Background(), request)
	if err == nil || err.Code != "project_operation_failed" || first.Replayed || !first.Changed {
		t.Fatalf("unexpected restart result: %#v %v", first, err)
	}
	if first.Items[0].Status != "running" || first.Items[0].Item == nil || first.Items[0].Item.Previous != web.ID {
		t.Fatalf("active command was not restarted: %#v", first.Items[0])
	}
	if first.Items[1].Status != "failed" || first.Items[1].Condition == nil || first.Items[1].Condition.Code != "process_not_found" {
		t.Fatalf("missing command was not preserved as a condition: %#v", first.Items[1])
	}
	if len(processes.calls) != 1 || processes.calls[0].ID != web.ID {
		t.Fatalf("restart did not freeze the original target: %#v", processes.calls)
	}

	processes.mu.Lock()
	processes.records = append(processes.records, runningRecord(p, "api", tasks.ID()))
	processes.mu.Unlock()
	second, err := manager.Apply(context.Background(), request)
	if err == nil || err.Code != "project_operation_failed" || !second.Replayed || len(processes.calls) != 1 {
		t.Fatalf("restart retry re-enumerated targets: %#v %v calls=%#v", second, err, processes.calls)
	}
}

func TestRestartPendingRetryUsesSameTargetAndChildRequest(t *testing.T) {
	p := testProject(t, "web")
	web := runningRecord(p, "web", tasks.ID())
	processes := &fakeProcesses{records: []services.Record{web}}
	processes.apply = func(request services.Request, count int) (services.Result, *protocol.Error) {
		record := runningRecord(p, "web", request.RequestID)
		record.Previous = web.ID
		if count == 1 {
			record.State = "starting"
			return services.Result{Item: record, Changed: true}, protocol.NewError("process_pending", "Restart is pending.", 3, nil)
		}
		return services.Result{Item: record}, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionRestart, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}

	first, err := manager.Apply(context.Background(), request)
	if err == nil || err.Code != "project_operation_failed" || first.Items[0].Status != "pending" || !first.Changed {
		t.Fatalf("pending restart was not persisted: %#v %v", first, err)
	}
	second, err := manager.Apply(context.Background(), request)
	if err != nil || !second.Replayed || second.Items[0].Status != "running" || !second.Changed {
		t.Fatalf("pending restart did not converge: %#v %v", second, err)
	}
	if len(processes.calls) != 2 || processes.calls[0].ID != web.ID || processes.calls[1].ID != web.ID || processes.calls[0].RequestID != processes.calls[1].RequestID {
		t.Fatalf("pending retry changed target or child request: %#v", processes.calls)
	}
}

func TestRestartPartialFailureRetriesOnlyIncompleteChild(t *testing.T) {
	p := testProject(t, "web", "api")
	web := runningRecord(p, "web", tasks.ID())
	api := runningRecord(p, "api", tasks.ID())
	processes := &fakeProcesses{records: []services.Record{web, api}}
	attempts := map[string]int{}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		attempts[request.ID]++
		if request.BeforeStart == nil {
			t.Fatalf("restart lost preflight callback: %#v", request)
		}
		if err := request.BeforeStart(context.Background()); err != nil {
			return services.Result{}, err
		}
		if request.ID == api.ID && attempts[request.ID] == 1 {
			return services.Result{}, protocol.NewError("execution_failed", "API restart failed.", 126, nil)
		}
		command := "web"
		if request.ID == api.ID {
			command = "api"
		}
		record := runningRecord(p, command, request.RequestID)
		record.Previous = request.ID
		return services.Result{Item: record, Changed: true}, nil
	}
	manager := Manager{
		Data:      t.TempDir(),
		Processes: processes,
		Preflight: func(context.Context, project.Context, string, *string) *protocol.Error { return nil },
	}
	request := Request{Action: ActionRestart, Project: p, Commands: []string{"web", "api"}, Timeout: time.Second, RequestID: tasks.ID()}

	first, err := manager.Apply(context.Background(), request)
	if err == nil || err.Code != "project_operation_failed" || first.Items[0].Status != "running" || first.Items[1].Status != "failed" {
		t.Fatalf("unexpected partial restart: %#v %v", first, err)
	}
	if len(processes.calls) != 2 {
		t.Fatalf("initial restart call count = %d", len(processes.calls))
	}
	firstAPIRequest := processes.calls[1].RequestID

	second, err := manager.Apply(context.Background(), request)
	if err != nil || !second.Replayed || second.Items[0].Status != "running" || second.Items[1].Status != "running" {
		t.Fatalf("restart retry did not converge: %#v %v", second, err)
	}
	if len(processes.calls) != 3 || processes.calls[2].ID != api.ID || processes.calls[2].RequestID == firstAPIRequest {
		t.Fatalf("restart repeated completed child or reused terminal child request: %#v", processes.calls)
	}
}

func TestRestartPreflightUsesInheritedEnvOverride(t *testing.T) {
	p := testProject(t, "web")
	web := runningRecord(p, "web", tasks.ID())
	inherited := "staging"
	web.Env = inherited
	web.EnvOverride = &inherited
	processes := &fakeProcesses{records: []services.Record{web}}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		if request.BeforeStart == nil {
			t.Fatal("restart lost preflight callback")
		}
		if err := request.BeforeStart(context.Background()); err != nil {
			return services.Result{}, err
		}
		record := runningRecord(p, "web", request.RequestID)
		record.Env = inherited
		record.EnvOverride = &inherited
		record.Previous = web.ID
		return services.Result{Item: record, Changed: true}, nil
	}
	var observed *string
	manager := Manager{
		Data:      t.TempDir(),
		Processes: processes,
		Preflight: func(_ context.Context, _ project.Context, _ string, env *string) *protocol.Error {
			observed = env
			return nil
		},
	}
	request := Request{Action: ActionRestart, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}
	result, err := manager.Apply(context.Background(), request)
	if err != nil || len(result.Items) != 1 || result.Items[0].Status != "running" {
		t.Fatalf("restart failed: %#v %v", result, err)
	}
	if observed == nil || *observed != inherited {
		t.Fatalf("preflight env = %v, want inherited %q", observed, inherited)
	}
}

func TestUpUsesActualStartedProcessReadinessConfiguration(t *testing.T) {
	p := testProject(t, "web")
	processes := &fakeProcesses{}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		record := runningRecord(p, request.Command, request.RequestID)
		record.ReadyConfigured = true
		return services.Result{Item: record, Changed: true}, nil
	}
	processes.wait = func(id string, _ int) (services.Record, *protocol.Error) {
		record := runningRecord(p, "web", id)
		record.ReadyConfigured = true
		record.Readiness = &services.Readiness{Ready: true, CheckedAt: time.Now().UTC(), Reason: "probe_passed"}
		return record, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionUp, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}

	result, err := manager.Apply(context.Background(), request)
	if err != nil || len(result.Items) != 1 || result.Items[0].Status != "ready" {
		t.Fatalf("actual readiness configuration was ignored: %#v %v", result, err)
	}
	if len(processes.waitCalls) != 1 {
		t.Fatalf("readiness wait calls = %#v", processes.waitCalls)
	}
}

func TestRestartUsesActualStartedProcessReadinessConfiguration(t *testing.T) {
	p := testProject(t, "web")
	web := runningRecord(p, "web", tasks.ID())
	processes := &fakeProcesses{records: []services.Record{web}}
	processes.apply = func(request services.Request, _ int) (services.Result, *protocol.Error) {
		record := runningRecord(p, "web", request.RequestID)
		record.Previous = web.ID
		record.ReadyConfigured = true
		return services.Result{Item: record, Changed: true}, nil
	}
	processes.wait = func(id string, _ int) (services.Record, *protocol.Error) {
		record := runningRecord(p, "web", id)
		record.ReadyConfigured = true
		record.Readiness = &services.Readiness{Ready: true, CheckedAt: time.Now().UTC(), Reason: "probe_passed"}
		return record, nil
	}
	manager := Manager{Data: t.TempDir(), Processes: processes}
	request := Request{Action: ActionRestart, Project: p, Commands: []string{"web"}, Timeout: time.Second, RequestID: tasks.ID()}

	result, err := manager.Apply(context.Background(), request)
	if err != nil || len(result.Items) != 1 || result.Items[0].Status != "ready" {
		t.Fatalf("restart actual readiness configuration was ignored: %#v %v", result, err)
	}
	if len(processes.waitCalls) != 1 {
		t.Fatalf("restart readiness wait calls = %#v", processes.waitCalls)
	}
}
