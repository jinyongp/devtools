package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestForegroundExecutionUsesProcessInventoryAndStop(t *testing.T) {
	app := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "child-ready")
	args, _ := json.Marshal([]string{executable, "-test.run=^TestForegroundExecutionChild$", "--", ready})
	probe, _ := json.Marshal([]string{executable, "-test.run=^TestForegroundExecutionChild$", "--", "probe"})
	config := "profile='app'\n[ports.web]\n[commands.web]\nexec=" + string(args) + "\nserve=['web']\nenv='local'\nbundle='all'\n[commands.web.ready]\nexec=" + string(probe) + "\n"
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	outputData(t, app, []string{"env", "create", "local"})
	store, condition := app.serviceStore()
	if condition != nil {
		t.Fatal(condition)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan struct{})
	code := 0
	go func() {
		defer close(done)
		code = app.Run(ctx, []string{"run", "--env", "", "--bundle", "none", "web"}, IO{Out: &stdout, Err: &stderr})
	}()
	defer func() { cancel(); <-done }()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-done:
			t.Fatalf("run exited before readiness: %d %s", code, stderr.String())
		case <-ctx.Done():
			t.Fatal("foreground command did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	var rows []services.NavigationRow
	for {
		rows, condition = store.Navigation(ctx, services.NavigationSelection{Profile: "app", Directory: dir}, nil)
		if condition != nil {
			t.Fatal(condition)
		}
		if len(rows) == 1 && rows[0].ActiveCount == 1 && rows[0].State == "running" && rows[0].ExecutionID != "" {
			break
		}
		// The native child can publish its own readiness before the
		// controller finishes publishing the execution's running record.
		select {
		case <-done:
			t.Fatalf("run exited before running publication: %d %s", code, stderr.String())
		case <-ctx.Done():
			t.Fatalf("foreground inventory did not become running: %#v", rows)
		case <-time.After(10 * time.Millisecond):
		}
	}
	id := rows[0].ExecutionID
	status, condition := store.Status(ctx, id)
	if condition != nil || status.Mode != "foreground" || status.ID != id || status.Control.State != "available" || !status.Control.Stop || status.Cleanup.State != "not_requested" {
		t.Fatalf("foreground status: %#v %v", status, condition)
	}
	if status.Env != "" || status.EnvOverride == nil || *status.EnvOverride != "" || status.BundleOverride == nil || *status.BundleOverride != "none" {
		t.Fatalf("foreground registration lost explicit common env/bundle selection: %#v", status)
	}
	entry := outputData(t, New("test", "test"), []string{"schema", "process", "status"})
	itemSchema := entry["output_schema"].(map[string]any)["properties"].(map[string]any)["item"]
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("urn:foreground:item", itemSchema); err != nil {
		t.Fatal(err)
	}
	contract, err := compiler.Compile("urn:foreground:item")
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var actual any
	if err := json.Unmarshal(value, &actual); err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(actual); err != nil {
		t.Fatalf("actual foreground record violates the published schema: %v", err)
	}
	check, condition := store.Check(ctx, id)
	if condition != nil || check.Readiness == nil || !check.Readiness.Ready {
		t.Fatalf("foreground readiness: %#v %v", check, condition)
	}
	inspection := outputData(t, app, []string{"project", "status", "web"})
	validateProjectInspection(t, app, inspection)
	observed := inspection["observations"].([]any)[0].(map[string]any)
	if observed["state"] != "controlled" || observed["execution_id"] != id || observed["control"].(map[string]any)["stop"] != true {
		t.Fatal("project inspection did not authenticate the foreground controller", inspection)
	}
	remedy := conflictStopAction(t, app, id)
	stopped := outputData(t, app, remedy.Argv[1:])
	item := stopped["item"].(map[string]any)
	if item["state"] != "stopped" || stopped["changed"] != true || item["cleanup"].(map[string]any)["state"] != "complete" {
		t.Fatalf("copied foreground stop did not verify cleanup: %#v", stopped)
	}
	request := services.Request{Action: "stop", ID: id, RequestID: stopped["request_id"].(string)}
	replay, condition := store.Apply(ctx, request)
	if condition != nil || !replay.Replayed || replay.Item.ID != id {
		t.Fatalf("foreground replay: %#v %v", replay, condition)
	}
	<-done
	if code == 0 || stdout.String() != "native child output\n" || stderr.Len() != 0 {
		t.Fatalf("native streams and exit: %d %q %q", code, stdout.String(), stderr.String())
	}
	replacement, condition := store.Apply(ctx, services.Request{Action: "restart", ID: id, RequestID: tasks.ID()})
	if replacement.Item.ID != "" {
		defer store.Apply(context.Background(), services.Request{Action: "stop", ID: replacement.Item.ID, RequestID: tasks.ID()})
	}
	if condition != nil || replacement.Item.ID == id || replacement.Item.Mode != "managed" || replacement.Item.Env != "" || replacement.Item.EnvOverride == nil || *replacement.Item.EnvOverride != "" || replacement.Item.BundleOverride == nil || *replacement.Item.BundleOverride != "none" {
		t.Fatalf("foreground restart changed the explicit selection: %#v %v", replacement, condition)
	}
	if _, condition := store.Apply(ctx, services.Request{Action: "stop", ID: id, RequestID: tasks.ID()}); condition != nil {
		t.Fatal(condition)
	}
	if live, condition := store.Status(ctx, replacement.Item.ID); condition != nil || live.State != "running" {
		t.Fatalf("old execution stop affected its replacement: %#v %v", live, condition)
	}
	managedRemedy := conflictStopAction(t, app, replacement.Item.ID)
	managedStop := outputData(t, app, managedRemedy.Argv[1:])
	if managedStop["item"].(map[string]any)["cleanup"].(map[string]any)["state"] != "complete" {
		t.Fatal("copied managed stop did not verify cleanup", managedStop)
	}
}

func conflictStopAction(t *testing.T, app *App, id string) protocol.Remedy {
	t.Helper()
	code, _, diagnostic := invoke(t, app, "", "run", "--env", "", "--bundle", "none", "web")
	var response struct {
		Error struct {
			Details struct{ Remedies []protocol.Remedy }
		}
	}
	if code != 3 || json.Unmarshal([]byte(diagnostic), &response) != nil || len(response.Error.Details.Remedies) != 1 {
		t.Fatalf("conflict did not offer one concrete action: %d %s", code, diagnostic)
	}
	remedy := response.Error.Details.Remedies[0]
	if remedy.State != "ready" || remedy.Effect != "system_write" || !reflect.DeepEqual(remedy.Argv, []string{"devtools", "process", "stop", id}) || app.ResolveAction(remedy.Argv) != nil {
		t.Fatal("authenticated control did not produce the exact supported stop command", remedy)
	}
	return remedy
}

func TestForegroundExecutionChild(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	path := os.Args[len(os.Args)-1]
	if path == "probe" {
		os.Exit(0)
	}
	if filepath.Base(path) != "child-ready" {
		return
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	_, _ = os.Stdout.WriteString("native child output\n")
	if os.WriteFile(path, nil, 0600) != nil {
		os.Exit(91)
	}
	select {
	case <-interrupt:
		os.Exit(42)
	case <-time.After(30 * time.Second):
		os.Exit(92)
	}
}
