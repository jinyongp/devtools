package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
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
