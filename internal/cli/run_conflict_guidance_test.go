package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRunConflictInspectionIsExecutableWithoutTheOriginalDirectory(t *testing.T) {
	for _, hasClaim := range []bool{true, false} {
		t.Run(fmt.Sprint(hasClaim), func(t *testing.T) {
			app := testApp(t)
			dir := privateTempDir(t)
			t.Chdir(dir)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			config := fmt.Sprintf("profile='app'\n[ports.web]\nport=%d\nstrict=true\n[commands.web]\nexec=%s\nserve=['web']\n", listener.Addr().(*net.TCPAddr).Port, fixtureExec(t, "true"))
			if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			// Reserve the assignment before testing a conflicting listener or an
			// older foreground controller that has no execution record.
			assignment := outputData(t, app, []string{"port", "allocate", "web"})["item"].(map[string]any)
			if hasClaim {
				store, condition := app.portStore()
				if condition != nil {
					t.Fatal(condition)
				}
				store.Owner = ports.RunOwner{Kind: "foreground", Command: "web"}
				release, condition := store.Claim(context.Background(), assignment["instance_id"].(string), []string{"web"})
				if condition != nil {
					t.Fatal(condition)
				}
				defer release()
			}
			app.portProbe = nil
			code, stdout, diagnostic := invoke(t, app, "", "run", "web")
			if code != 3 || stdout != "" {
				t.Fatal(code, stdout, diagnostic)
			}
			var response struct {
				Error struct {
					Details struct{ Remedies []protocol.Remedy }
				}
			}
			if err := json.Unmarshal([]byte(diagnostic), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Error.Details.Remedies) != 1 {
				t.Fatal(diagnostic)
			}
			action := response.Error.Details.Remedies[0]
			if action.State != "ready" || action.Effect != "read" || app.ResolveAction(action.Argv) != nil {
				t.Fatal("inspection is not accepted by the real command parser", action)
			}
			// A command shown in an error must work when copied into another
			// terminal, including one whose cwd has no project configuration.
			t.Chdir(privateTempDir(t))
			data := outputData(t, app, action.Argv[1:])
			validateProjectInspection(t, app, data)
			if data["profile"] != "app" || data["directory"] != dir || len(data["observations"].([]any)) != 1 {
				t.Fatal("inspection selected another execution location", data)
			}
			observed := data["observations"].([]any)[0].(map[string]any)
			state := "unmanaged"
			if hasClaim {
				state = "untracked"
			}
			if observed["state"] != state || observed["control"].(map[string]any)["stop"] != false || len(observed["listener"].(map[string]any)["processes"].([]any)) == 0 {
				t.Fatal("inspection lost the actual listener or invented control", observed)
			}
			t.Chdir(dir)
			code, stdout, diagnostic = invoke(t, app, "", "--format", "text", "run", "web")
			if code != 3 || stdout != "" || !strings.Contains(diagnostic, "devtools project status web") || !strings.Contains(diagnostic, filepath.Base(dir)) || strings.Contains(diagnostic, "Identify its controller") || strings.Contains(diagnostic, "--view") || strings.Contains(diagnostic, "--state") {
				t.Fatal(code, stdout, diagnostic)
			}
			if hasClaim && !strings.Contains(diagnostic, "not registered") {
				t.Fatal("older foreground run lost its explanation", diagnostic)
			}
		})
	}
}

func validateProjectInspection(t *testing.T, app *App, data map[string]any) {
	t.Helper()
	entry := outputData(t, app, []string{"schema", "project", "status"})
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("urn:project-inspection", entry["output_schema"]); err != nil {
		t.Fatal(err)
	}
	contract, err := compiler.Compile("urn:project-inspection")
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(data); err != nil {
		t.Fatalf("actual inspection violates its published schema: %v\n%#v", err, data)
	}
}

func TestProjectInspectionKeepsListenerWhenRecordsRequireUpgrade(t *testing.T) {
	app := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config := fmt.Sprintf("profile='app'\n[ports.web]\nport=%d\nstrict=true\n[commands.web]\nexec=%s\nserve=['web']\n", listener.Addr().(*net.TCPAddr).Port, fixtureExec(t, "true"))
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app.portProbe = nil
	store, condition := app.serviceStore()
	if condition != nil {
		t.Fatal(condition)
	}
	id := tasks.ID()
	now := time.Now().UTC()
	legacy, _ := json.Marshal(services.Record{ID: id, Profile: "app", Directory: dir, Command: "web", State: "failed", CreatedAt: now, EndedAt: &now})
	path := filepath.Join(store.Data, "processes", id, "record.json")
	if err := maintenance.Write(path, legacy); err != nil {
		t.Fatal(err)
	}
	data := outputData(t, app, []string{"project", "status", "web"})
	validateProjectInspection(t, app, data)
	observed := data["observations"].([]any)[0].(map[string]any)
	if data["complete"] != false || observed["state"] != "unmanaged" || observed["control"].(map[string]any)["stop"] != false || len(observed["listener"].(map[string]any)["processes"].([]any)) == 0 {
		t.Fatal(data)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, legacy) {
		t.Fatal("read-only inspection rewrote legacy storage")
	}
	actions := data["next_actions"].([]any)
	if len(actions) != 1 {
		t.Fatal(data)
	}
	action := actions[0].(map[string]any)
	var argv []string
	for _, value := range action["argv"].([]any) {
		argv = append(argv, value.(string))
	}
	if app.ResolveAction(argv) != nil || !strings.Contains(strings.Join(argv, " "), "doctor --storage --fix") {
		t.Fatal(action)
	}
	outputData(t, app, argv[1:])
	after := outputData(t, app, []string{"project", "status", "web"})
	validateProjectInspection(t, app, after)
	if after["complete"] != true || len(after["items"].([]any)) != 0 {
		t.Fatal("upgrade did not restore inspection or exposed ended failures", after)
	}
}

func TestProjectInspectionCancellationPreservesPartialReport(t *testing.T) {
	app := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n[commands.web]\nexec="+fixtureExec(t, "true")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, condition := app.serviceStore()
	if condition != nil {
		t.Fatal(condition)
	}
	now := time.Now().UTC()
	if err := store.RestoreRecord(services.Record{ID: tasks.ID(), Profile: "app", Directory: dir, Command: "web", State: "running", CreatedAt: now, StartedAt: &now}); err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.LockExclusive(context.Background(), filepath.Join(store.Data, ".retained-bodies.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(50*time.Millisecond, cancel)
	defer timer.Stop()
	var out, diagnostic bytes.Buffer
	code := app.Run(ctx, []string{"project", "status", "web"}, IO{Out: &out, Err: &diagnostic})
	var response struct {
		Error struct {
			Details struct{ Report map[string]any }
		}
	}
	if code != 130 || out.Len() != 0 || json.Unmarshal(diagnostic.Bytes(), &response) != nil || response.Error.Details.Report["complete"] != false {
		t.Fatalf("cancellation lost the partial report or exit contract: %d %q", code, diagnostic.String())
	}
	validateProjectInspection(t, app, response.Error.Details.Report)
}
