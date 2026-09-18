package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
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
