package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/installstorage"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestHomebrewUpdatePreservesInstallation(t *testing.T) {
	previous := updateManager
	updateManager = "homebrew"
	t.Cleanup(func() { updateManager = previous })
	// A missing executable/source proves refusal happens before running the installer.
	result, err := updateExecutable(context.Background(), "/missing/devtools", map[string]string{"source": "/missing", "version": "0.1.0"})
	if result != nil || err == nil || err.Code != "package_managed" || err.ExitCode != 3 || err.Details["command"] != "brew upgrade jinyongp/tap/devtools" {
		t.Fatalf("unexpected result: %v, %v", result, err)
	}
}

func TestNoninteractiveUpdateFailureKeepsDiagnosticStreamJSONOnly(t *testing.T) {
	root := t.TempDir()
	var diagnostic bytes.Buffer
	_, e := updateExecutable(context.Background(), filepath.Join(root, "devtools"), map[string]string{"source": root, "version": "0.0.0-test.1"}, IO{Err: &diagnostic})
	if e == nil || e.Code != "update_failed" || diagnostic.Len() != 0 {
		t.Fatal(e, diagnostic.String())
	}
}

func TestUpdateReportsUnreadableResultAndResumesWithInstalledVersion(t *testing.T) {
	app := presentationApp(presentation.Environment{})
	for i := range app.commands {
		if app.commands[i].Name == "update" {
			app.commands[i].Run = func(context.Context, IO, Request) (any, *protocol.Error) {
				storage := installstorage.Failed("storage_upgrade_result_invalid")
				return finishExecutableUpdate(map[string]any{"version": "0.49.0", "storage": storage}, storage, true)
			}
			break
		}
	}
	var out, diagnostic bytes.Buffer
	code := app.Run(context.Background(), []string{"--format", "text", "update"}, IO{Out: &out, Err: &diagnostic})
	text := diagnostic.String()
	if code != 3 || out.Len() != 0 || !strings.Contains(text, "0.49.0") || !strings.Contains(text, "could not read the installed executable's upgrade result") || !strings.Contains(text, "devtools update --resume") || strings.Contains(text, "doctor --storage --fix") {
		t.Fatalf("unexpected update diagnostic: %d %q %q", code, out.String(), text)
	}
}
