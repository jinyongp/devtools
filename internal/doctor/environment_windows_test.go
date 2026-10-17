package doctor

import (
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/values"
)

func TestWindowsDoctorReportsEnvironmentCaseKeysOnly(t *testing.T) {
	var state values.State
	fixture := `{"version":1,"profile":"test","envs":{},"keys":{"Path":{"kind":"variable","common":"private-one","envs":{}},"PATH":{"kind":"variable","common":"private-two","envs":{}}}}`
	if err := json.Unmarshal([]byte(fixture), &state); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	called := false
	ctx := process.WithRunner(context.Background(), func(context.Context, []string, string, []string, io.Reader, io.Writer, io.Writer) (int, *protocol.Error) {
		called = true
		return 0, nil
	})
	checks := CheckRequirements(ctx, Input{Directory: t.TempDir(), Inject: true, State: &state, Requirements: project.Requirements{Tools: map[string]project.Tool{"fixture": {Executable: exe, Version: "1.2.3"}}}})
	if called {
		t.Fatal("doctor probed a tool after detecting an ambiguous environment")
	}
	found := false
	for _, check := range checks {
		if check.ID == "environment:names" && check.Status == "fail" {
			found = true
			if !strings.Contains(check.Message, "Path") || !strings.Contains(check.Message, "PATH") {
				t.Fatal("conflicting key names absent")
			}
		}
	}
	if !found {
		t.Fatal("doctor did not diagnose ambiguous environment")
	}
	data, _ := json.Marshal(checks)
	if strings.Contains(string(data), "private-") {
		t.Fatal("doctor exposed environment values")
	}
}
