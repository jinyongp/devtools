package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/lifecycle"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

type cliLifecycleProcesses struct {
	mu      sync.Mutex
	records []services.Record
	calls   []services.Request
}

func (f *cliLifecycleProcesses) Apply(_ context.Context, request services.Request) (services.Result, *protocol.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request)
	switch request.Action {
	case "start":
		env := ""
		if request.Env != nil {
			env = *request.Env
		}
		capture := request.Capture != nil && *request.Capture
		for _, record := range f.records {
			if record.Directory == request.Directory && record.Command == request.Command && record.EndedAt == nil {
				if record.Env != env || record.Capture != capture {
					return services.Result{}, protocol.NewError("process_conflict", "Existing execution uses different options.", 3, nil)
				}
				return services.Result{Item: record}, nil
			}
		}
		now := time.Now().UTC()
		record := services.Record{ID: request.RequestID, Profile: "app", Instance: "instance", Directory: request.Directory, Command: request.Command, Env: env, EnvOverride: request.Env, Capture: capture, CreatedAt: now, StartedAt: &now, State: "running"}
		f.records = append(f.records, record)
		return services.Result{Item: record, Changed: true}, nil
	case "stop":
		for index := range f.records {
			if f.records[index].ID != request.ID {
				continue
			}
			if f.records[index].EndedAt != nil {
				return services.Result{Item: f.records[index]}, nil
			}
			now := time.Now().UTC()
			f.records[index].EndedAt = &now
			f.records[index].State = "stopped"
			return services.Result{Item: f.records[index], Changed: true}, nil
		}
		return services.Result{}, protocol.NewError("process_not_found", "Execution not found.", 3, nil)
	default:
		return services.Result{}, protocol.NewError("invalid_argument", "Unsupported fake action.", 2, nil)
	}
}

func (f *cliLifecycleProcesses) List(_ context.Context, profile string) ([]services.Record, *protocol.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := []services.Record{}
	for _, record := range f.records {
		if profile == "" || record.Profile == profile {
			items = append(items, record)
		}
	}
	return items, nil
}

func (f *cliLifecycleProcesses) Wait(_ context.Context, id string, _ time.Duration) (services.Record, *protocol.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, record := range f.records {
		if record.ID == id {
			return record, nil
		}
	}
	return services.Record{}, protocol.NewError("process_not_found", "Execution not found.", 3, nil)
}

func (f *cliLifecycleProcesses) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestProjectLifecycleCLI(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	config := `profile="app"
[commands.web]
exec=["/bin/sh","-c","true"]
[commands.api]
exec=["/bin/sh","-c","true"]
`
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	processes := &cliLifecycleProcesses{}
	app.lifecycleManager = func(data string) lifecycle.Manager { return lifecycle.Manager{Data: data, Processes: processes} }
	if code, _, diagnostic := invoke(t, app, "", "env", "create", "dev"); code != 0 {
		t.Fatal(diagnostic)
	}

	requestID := tasks.ID()
	upArgs := []string{"project", "up", "web", "api", "--env", "dev", "--capture-logs", "--request-id", requestID}
	up := outputData(t, app, upArgs)
	if up["action"] != "up" || up["profile"] != "app" || up["directory"] != root || up["changed"] != true || up["replayed"] != false {
		t.Fatalf("unexpected project up result: %#v", up)
	}
	items := up["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["command"] != "web" || items[1].(map[string]any)["command"] != "api" {
		t.Fatalf("unexpected project up items: %#v", items)
	}
	if processes.callCount() != 2 {
		t.Fatalf("unexpected start calls: %#v", processes.calls)
	}
	for _, call := range processes.calls {
		if call.Env == nil || *call.Env != "dev" || call.Capture == nil || !*call.Capture {
			t.Fatalf("project options not forwarded: %#v", call)
		}
	}

	replay := outputData(t, app, upArgs)
	if replay["replayed"] != true || processes.callCount() != 2 {
		t.Fatalf("project up retry repeated children: %#v calls=%#v", replay, processes.calls)
	}

	noOp := outputData(t, app, []string{"project", "up", "web", "--env", "dev", "--capture-logs", "--request-id", tasks.ID()})
	if noOp["changed"] != false || noOp["items"].([]any)[0].(map[string]any)["status"] != "unchanged" {
		t.Fatalf("existing singleton did not converge to no-op: %#v", noOp)
	}
	code, out, diagnostic := invoke(t, app, "", "project", "up", "web", "--request-id", tasks.ID())
	if code != 3 || out != "" || !strings.Contains(diagnostic, "process_conflict") {
		t.Fatalf("different env/capture did not conflict: code=%d out=%s err=%s", code, out, diagnostic)
	}

	status := outputData(t, app, []string{"project", "status"})
	if status["profile"] != "app" || status["directory"] != root || len(status["items"].([]any)) != 2 {
		t.Fatalf("unexpected project status: %#v", status)
	}
	selected := outputData(t, app, []string{"project", "status", "web"})
	if len(selected["items"].([]any)) != 1 || selected["items"].([]any)[0].(map[string]any)["command"] != "web" {
		t.Fatalf("project status selection failed: %#v", selected)
	}
	if strings.Contains(strings.ToLower(mustJSON(t, status)), "output.log") {
		t.Fatal("project status exposed log content/path")
	}

	downWeb := outputData(t, app, []string{"project", "down", "web", "--request-id", tasks.ID()})
	if downWeb["changed"] != true || downWeb["items"].([]any)[0].(map[string]any)["status"] != "stopped" {
		t.Fatalf("project down selection failed: %#v", downWeb)
	}
	status = outputData(t, app, []string{"project", "status"})
	if len(status["items"].([]any)) != 1 || status["items"].([]any)[0].(map[string]any)["command"] != "api" {
		t.Fatalf("selected down stopped the wrong process: %#v", status)
	}
	downAll := outputData(t, app, []string{"project", "down", "--request-id", tasks.ID()})
	if downAll["changed"] != true || len(downAll["items"].([]any)) != 1 {
		t.Fatalf("project down all failed: %#v", downAll)
	}
	if status = outputData(t, app, []string{"project", "status"}); len(status["items"].([]any)) != 0 {
		t.Fatalf("project status remained active after down: %#v", status)
	}
}

func TestProjectUpPreflightDoesNotMutateOnMissingSecret(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	config := `profile="app"
[commands.api]
exec=["/bin/sh","-c","true"]
inject=true
[commands.api.requirements]
secs=["TOKEN"]
`
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	processes := &cliLifecycleProcesses{}
	app.lifecycleManager = func(data string) lifecycle.Manager { return lifecycle.Manager{Data: data, Processes: processes} }
	requestID := tasks.ID()
	args := []string{"project", "up", "api", "--request-id", requestID}
	code, out, diagnostic := invoke(t, app, "", args...)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "requirements_failed") || processes.callCount() != 0 {
		t.Fatalf("missing secret mutated processes: code=%d out=%s err=%s calls=%#v", code, out, diagnostic, processes.calls)
	}
	if code, _, diagnostic := invoke(t, app, "PROJECT_LIFECYCLE_SECRET", "sec", "set", "TOKEN", "--stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	result := outputData(t, app, args)
	if result["changed"] != true || processes.callCount() != 1 || strings.Contains(mustJSON(t, result), "PROJECT_LIFECYCLE_SECRET") {
		t.Fatalf("project up after fixing preflight failed or leaked secret: %#v calls=%#v", result, processes.calls)
	}
}

func TestProjectUpServeOnlyCommandDoesNotRequireUnusedEnvStorage(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	config := `profile="app"
[ports.web]
port=25400
range=[25400,25499]
[commands.web]
exec=["/bin/sh","-c","true"]
serve=["web"]
env="missing"
[commands.web.requirements.tools.shell]
executable="/bin/sh"
`
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	processes := &cliLifecycleProcesses{}
	app.lifecycleManager = func(data string) lifecycle.Manager { return lifecycle.Manager{Data: data, Processes: processes} }

	result := outputData(t, app, []string{"project", "up", "web", "--request-id", tasks.ID()})
	if result["changed"] != true || processes.callCount() != 1 {
		t.Fatalf("serve-only project up should not require an unused env: %#v calls=%#v", result, processes.calls)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
