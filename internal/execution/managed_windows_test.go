package execution

import (
	"context"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsManagedBridgePreflightBeforePortProbe(t *testing.T) {
	dir := t.TempDir()
	bridge := filepath.Join(dir, "wsl.exe")
	if err := os.WriteFile(bridge, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	command := Command{Project: project.Context{Profile: "app", Root: dir, Ports: map[string]project.Port{"web": {}}}, Name: "web", Exec: []string{bridge}, Directory: dir, Serve: []string{"web"}}
	probes := 0
	dependencies := Dependencies{Ports: ports.Store{Directory: filepath.Join(dir, "ports"), Probe: func(int) (bool, *protocol.Error) { probes++; return true, nil }}, PortDefaults: []int{23000, 23100}}
	failure := Preflight(context.Background(), command, dependencies, os.Environ())
	if failure == nil || failure.Code != "requirements_failed" || !hasCauseCode(failure, "unsupported_platform") || probes != 0 {
		t.Fatalf("managed bridge reached port probe: %v probes=%d", failure, probes)
	}
	if _, err := os.Stat(dependencies.Ports.Directory); !os.IsNotExist(err) {
		t.Fatalf("managed bridge preflight created port state: %v", err)
	}
}
