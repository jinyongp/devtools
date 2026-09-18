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
	"github.com/jinyongp/devtools/internal/maintenance"
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
	case "restart":
		for index := range f.records {
			if f.records[index].ID != request.ID {
				continue
			}
			previous := f.records[index]
			ended := time.Now().UTC()
			if f.records[index].EndedAt == nil {
				f.records[index].EndedAt = &ended
				f.records[index].State = "stopped"
			}
			if request.BeforeStart != nil {
				if err := request.BeforeStart(context.Background()); err != nil {
					return services.Result{Item: f.records[index], Changed: true}, err
				}
			}
			env, envOverride := previous.Env, previous.EnvOverride
			if request.Env != nil {
				env, envOverride = *request.Env, request.Env
			}
			capture := previous.Capture
			if request.Capture != nil {
				capture = *request.Capture
			}
			now := time.Now().UTC()
			record := services.Record{
				ID:          request.RequestID,
				Profile:     previous.Profile,
				Instance:    previous.Instance,
				Directory:   previous.Directory,
				Command:     previous.Command,
				Env:         env,
				EnvOverride: envOverride,
				Capture:     capture,
				CreatedAt:   now,
				StartedAt:   &now,
				State:       "running",
				Previous:    previous.ID,
			}
			f.records = append(f.records, record)
			return services.Result{Item: record, Changed: true}, nil
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
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
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
	if up["action"] != "up" || up["profile"] != "app" || up["directory"] != canonicalRoot || up["changed"] != true || up["replayed"] != false {
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
	if status["profile"] != "app" || status["directory"] != canonicalRoot || len(status["items"].([]any)) != 2 {
		t.Fatalf("unexpected project status: %#v", status)
	}
	selected := outputData(t, app, []string{"project", "status", "web"})
	if len(selected["items"].([]any)) != 1 || selected["items"].([]any)[0].(map[string]any)["command"] != "web" {
		t.Fatalf("project status selection failed: %#v", selected)
	}
	if strings.Contains(strings.ToLower(mustJSON(t, status)), "output.log") {
		t.Fatal("project status exposed log content/path")
	}

	webBefore := selected["items"].([]any)[0].(map[string]any)["id"].(string)
	restartArgs := []string{"project", "restart", "web", "--env", "dev", "--capture-logs", "--request-id", tasks.ID()}
	restartedWeb := outputData(t, app, restartArgs)
	if restartedWeb["action"] != "restart" || restartedWeb["changed"] != true || restartedWeb["replayed"] != false {
		t.Fatalf("unexpected project restart result: %#v", restartedWeb)
	}
	restartedWebItem := restartedWeb["items"].([]any)[0].(map[string]any)
	if restartedWebItem["status"] != "running" {
		t.Fatalf("web restart did not return running: %#v", restartedWebItem)
	}
	webProcess := restartedWebItem["item"].(map[string]any)
	if webProcess["previous_id"] != webBefore || webProcess["env"] != "dev" || webProcess["capture_logs"] != true {
		t.Fatalf("web restart options/lineage drifted: %#v", webProcess)
	}
	replayedRestart := outputData(t, app, restartArgs)
	if replayedRestart["replayed"] != true {
		t.Fatalf("restart retry was not replayed: %#v", replayedRestart)
	}

	apiBefore := selectedProjectCommandID(t, status, "api")
	restartedAPI := outputData(t, app, []string{"project", "restart", "api", "--request-id", tasks.ID()})
	apiProcess := restartedAPI["items"].([]any)[0].(map[string]any)["item"].(map[string]any)
	if apiProcess["previous_id"] != apiBefore || apiProcess["env"] != "dev" || apiProcess["capture_logs"] != true {
		t.Fatalf("restart did not preserve omitted env/capture: %#v", apiProcess)
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

func TestProjectLifecycleCanonicalDirectoryIdentity(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "project-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	config := `profile="app"
[commands.web]
exec=["/bin/sh","-c","true"]
`
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	processes := &cliLifecycleProcesses{}
	app.lifecycleManager = func(data string) lifecycle.Manager { return lifecycle.Manager{Data: data, Processes: processes} }

	up := outputData(t, app, []string{"project", "up", "web", "--dir", alias, "--request-id", tasks.ID()})
	if up["directory"] != canonicalRoot || up["items"].([]any)[0].(map[string]any)["item"].(map[string]any)["directory"] != canonicalRoot {
		t.Fatalf("project up did not canonicalize alias: %#v", up)
	}
	status := outputData(t, app, []string{"project", "status", "--dir", root})
	if status["directory"] != canonicalRoot || len(status["items"].([]any)) != 1 || status["items"].([]any)[0].(map[string]any)["directory"] != canonicalRoot {
		t.Fatalf("canonical status did not find alias start: %#v", status)
	}
	down := outputData(t, app, []string{"project", "down", "--dir", alias, "--request-id", tasks.ID()})
	if down["directory"] != canonicalRoot || down["changed"] != true {
		t.Fatalf("alias down did not target canonical project: %#v", down)
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

func TestProjectLogsSelectsCurrentCanonicalProjectExecution(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	other := filepath.Join(base, "other")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "project-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalOther, err := filepath.EvalSymlinks(other)
	if err != nil {
		t.Fatal(err)
	}
	config := "profile='app'\n[commands.web]\nexec=['/bin/true']\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	currentID, otherID := tasks.ID(), tasks.ID()
	now := time.Now().UTC()
	current := services.Record{ID: currentID, Profile: "app", Instance: "current", Directory: canonicalRoot, Command: "web", Capture: true, CreatedAt: now, StartedAt: &now, State: "running"}
	otherRecord := services.Record{ID: otherID, Profile: "app", Instance: "other", Directory: canonicalOther, Command: "web", Capture: true, CreatedAt: now, StartedAt: &now, State: "running"}

	dataRoot := t.TempDir()
	app := New("test", "test")
	app.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(dataRoot, "profiles"), nil }
	processes := &cliLifecycleProcesses{records: []services.Record{otherRecord, current}}
	app.lifecycleManager = func(data string) lifecycle.Manager { return lifecycle.Manager{Data: data, Processes: processes} }

	for _, record := range []services.Record{current, otherRecord} {
		if err := os.MkdirAll(filepath.Join(dataRoot, "processes", record.ID), 0700); err != nil {
			t.Fatal(err)
		}
		if err := tasks.WritePrivate(filepath.Join(dataRoot, "processes", record.ID, "record.json"), record); err != nil {
			t.Fatal(err)
		}
		content := []byte("CURRENT")
		if record.ID == otherID {
			content = []byte("OTHER")
		}
		if err := maintenance.Write(filepath.Join(dataRoot, "processes", record.ID, "output.log"), content); err != nil {
			t.Fatal(err)
		}
	}
	result := outputData(t, app, []string{"project", "logs", "web", "--dir", alias})
	if result["id"] != currentID || result["command"] != "web" || result["content"] != "CURRENT" {
		t.Fatalf("project logs selected wrong execution: %#v", result)
	}
}

func TestProjectLogsRequiresOneActiveCapturedExecution(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='app'\n[commands.web]\nexec=['/bin/true']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dataRoot := t.TempDir()
	newApp := func(records []services.Record) *App {
		app := New("test", "test")
		app.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(dataRoot, "profiles"), nil }
		app.lifecycleManager = func(data string) lifecycle.Manager {
			return lifecycle.Manager{Data: data, Processes: &cliLifecycleProcesses{records: records}}
		}
		return app
	}

	code, out, diagnostic := invoke(t, newApp(nil), "", "project", "logs", "web", "--dir", root)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "process_not_found") {
		t.Fatalf("missing active execution: code=%d out=%q err=%q", code, out, diagnostic)
	}

	now := time.Now().UTC()
	uncaptured := services.Record{ID: tasks.ID(), Profile: "app", Instance: "one", Directory: canonicalRoot, Command: "web", Capture: false, CreatedAt: now, StartedAt: &now, State: "running"}
	if err := os.MkdirAll(filepath.Join(dataRoot, "processes", uncaptured.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := tasks.WritePrivate(filepath.Join(dataRoot, "processes", uncaptured.ID, "record.json"), uncaptured); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic = invoke(t, newApp([]services.Record{uncaptured}), "", "project", "logs", "web", "--dir", root)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "logs_disabled") {
		t.Fatalf("uncaptured execution: code=%d out=%q err=%q", code, out, diagnostic)
	}

	first := uncaptured
	first.ID = tasks.ID()
	first.Capture = true
	second := first
	second.ID = tasks.ID()
	code, out, diagnostic = invoke(t, newApp([]services.Record{first, second}), "", "project", "logs", "web", "--dir", root)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "process_conflict") {
		t.Fatalf("duplicate active execution: code=%d out=%q err=%q", code, out, diagnostic)
	}
}

func selectedProjectCommandID(t *testing.T, status map[string]any, command string) string {
	t.Helper()
	for _, raw := range status["items"].([]any) {
		item := raw.(map[string]any)
		if item["command"] == command {
			return item["id"].(string)
		}
	}
	t.Fatalf("project status does not contain %s: %#v", command, status)
	return ""
}
