package cli

import (
	"context"
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
