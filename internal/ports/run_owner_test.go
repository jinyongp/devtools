package ports

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestRunOwnerMetadataRejectsLegacyAndReusedPID(t *testing.T) {
	f, err := os.OpenFile(filepath.Join(privateTestRoot(t), "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	owner := func() map[string]any {
		return activeRunError(f).Details["facts"].(map[string]any)["owner"].(map[string]any)
	}
	if got := owner(); got["status"] != "unavailable" || got["reason"] != "legacy_run" {
		t.Fatal(got)
	}
	publishRunOwner(f, RunOwner{Kind: "foreground", Command: "dev:web"})
	if got := owner(); got["status"] != "verified" || got["pid"] != os.Getpid() || got["command"] != "dev:web" {
		t.Fatal(got)
	}
	_, _ = f.Seek(1, io.SeekStart)
	var metadata runMetadata
	if json.NewDecoder(f).Decode(&metadata) != nil {
		t.Fatal("metadata missing")
	}
	metadata.Identity = "previous-process-with-the-same-PID"
	if err := f.Truncate(1); err != nil {
		t.Fatal(err)
	}
	_, _ = f.Seek(1, io.SeekStart)
	if err := json.NewEncoder(f).Encode(metadata); err != nil {
		t.Fatal(err)
	}
	if got := owner(); got["status"] != "unavailable" || got["reason"] != "state_changed" || got["pid"] != nil {
		t.Fatal(got)
	}
	clearRunOwner(f)
	if got := owner(); got["reason"] != "legacy_run" {
		t.Fatal(got)
	}
}

func TestRunFailureKeepsAssignmentAndUnknownOwner(t *testing.T) {
	e := describeRunFailure(protocol.NewError("port_run_active", "Original.", 3, nil), "app", "/project", "instance", "web", 3000)
	facts := e.Details["facts"].(map[string]any)
	if facts["port"] != 3000 || facts["service"] != "web" || facts["owner"].(map[string]any)["status"] != "unavailable" {
		t.Fatal(facts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observed := observeRunFailure(ctx, e)
	if observed != e || observed.Code != "port_run_active" || facts["port_observation"].(map[string]any)["status"] != "unavailable" {
		t.Fatal(observed)
	}
}
