package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProcessStartPreflightRejectsBeforeExecutionRecord(t *testing.T) {
	data := t.TempDir()
	app := New("test", "test")
	app.dataDirectory = func() (string, *protocol.Error) {
		return filepath.Join(data, "profiles"), nil
	}
	root := t.TempDir()
	config := "profile='app'\n[requirements]\nvars=['REQUIRED']\n[commands.web]\nexec=['/bin/true']\ninject=true\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	requestID := tasks.ID()
	code, out, diagnostic := invoke(t, app, "", "process", "start", "web", "--dir", root, "--request-id", requestID)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"requirements_failed\"") {
		t.Fatalf("start: code=%d out=%q err=%q", code, out, diagnostic)
	}
	if _, err := os.Stat(filepath.Join(data, "processes", requestID, "record.json")); !os.IsNotExist(err) {
		t.Fatalf("execution record exists after failed preflight: %v", err)
	}
	state, err := (ports.Store{Directory: filepath.Join(data, "ports")}).Read()
	if err != nil || len(state.Instances) != 0 || len(state.Assignments) != 0 {
		t.Fatalf("failed preflight mutated port identity: %#v %v", state, err)
	}
}

func TestProjectPreflightReloadsCurrentConfiguration(t *testing.T) {
	data := t.TempDir()
	app := New("test", "test")
	app.dataDirectory = func() (string, *protocol.Error) {
		return filepath.Join(data, "profiles"), nil
	}
	root := t.TempDir()
	initial := "profile='app'\n[commands.web]\nexec=['/bin/true']\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, failure := project.Resolve(root, "")
	if failure != nil {
		t.Fatal(failure)
	}
	changed := "profile='app'\n[requirements]\nvars=['REQUIRED']\n[commands.web]\nexec=['/bin/true']\ninject=true\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	failure = app.preflightProjectCommand(context.Background(), snapshot, "web", nil)
	if failure == nil || failure.Code != "requirements_failed" {
		t.Fatalf("stale project configuration bypassed preflight: %v", failure)
	}
}

func TestProcessRestartPreStopValidationPreservesRunningExecution(t *testing.T) {
	data := t.TempDir()
	app := New("test", "test")
	app.dataDirectory = func() (string, *protocol.Error) {
		return filepath.Join(data, "profiles"), nil
	}
	root := t.TempDir()
	initial := "profile='app'\n[commands.web]\nexec=['/bin/true']\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, failure := project.Resolve(root, "")
	if failure != nil {
		t.Fatal(failure)
	}
	started := time.Now().UTC()
	id := tasks.ID()
	record := services.Record{
		ID:        id,
		Profile:   "app",
		Instance:  "instance",
		Directory: resolved.Root,
		Command:   "web",
		CreatedAt: started,
		StartedAt: &started,
		State:     "running",
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(data, "processes", id, "record.json")
	if err := maintenance.Write(recordPath, body); err != nil {
		t.Fatal(err)
	}
	changed := "profile='app'\n[commands.web]\nexec=['/definitely-not-an-installed-review-executable']\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}

	code, out, diagnostic := invoke(t, app, "", "process", "restart", id, "--request-id", tasks.ID())
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"requirements_failed\"") {
		t.Fatalf("restart: code=%d out=%q err=%q", code, out, diagnostic)
	}
	storedBytes, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored services.Record
	if err := json.Unmarshal(storedBytes, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != id || stored.EndedAt != nil || stored.State != "running" {
		t.Fatalf("pre-stop validation stopped the old process: %#v", stored)
	}
}
