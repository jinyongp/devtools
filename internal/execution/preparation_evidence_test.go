package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func TestPreparationConflictPreservesAuthenticatedSearchEvidence(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(executable)
	parent := []string{"PATH=" + filepath.Dir(executable)}
	c := Command{Project: project.Context{Profile: "app"}, Name: "web", Directory: privateTempDir(t), Inject: true, Exec: []string{name}, Requirements: project.Requirements{Tools: map[string]project.Tool{
		"curl": {Executable: name}, "node": {Executable: name}, "pnpm": {Executable: name},
	}}}
	state := values.DiagnosticPublicState("app", "", map[string]string{})
	cause := protocol.NewError("port_run_active", "Service web already has an active run.", 3, map[string]any{"remedies": []protocol.Remedy{}})
	for _, pending := range []bool{false, true} {
		if pending {
			c.Bind = map[string]project.Binding{"PATH": {Port: "web"}}
		}
		e := preparationFailure(context.Background(), c, parent, "foreground", state, cause)
		checks := e.Details["checks"].([]doctor.Check)
		found := 0
		for _, check := range checks {
			if check.ID != "command_executable" && check.Target["kind"] != "tool" {
				continue
			}
			found++
			if pending {
				if check.Status != "unknown" || len(check.BlockedBy) != 1 || check.BlockedBy[0] != "c1" {
					t.Fatal(check)
				}
			} else if check.Status != "pass" || len(check.BlockedBy) != 0 {
				t.Fatal(check)
			}
		}
		if found != 4 || e.Details["primary_cause_id"] != "c1" {
			t.Fatal(found, e)
		}
	}
}
