package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func previewAction(t *testing.T, value map[string]any) protocol.Remedy {
	t.Helper()
	b, e := json.Marshal(value["next_actions"])
	if e != nil {
		t.Fatal(e)
	}
	var actions []protocol.Remedy
	if e := json.Unmarshal(b, &actions); e != nil || len(actions) != 1 {
		t.Fatal(string(b), e)
	}
	return actions[0]
}

func TestPreviewReadyArgvAppliesExactChildAndRoot(t *testing.T) {
	a := testApp(t)
	projectDir, root := privateTempDir(t), privateTempDir(t)
	t.Chdir(projectDir)
	if e := os.WriteFile("devtools.toml", []byte("profile='app-a'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	child := []string{"/bin/echo", "public message", "--isolated-root", "literal child argument"}
	args := append([]string{"--isolated-root", root, "command", "add", "web", "--dry-run", "--"}, child...)
	preview := outputData(t, a, args)
	action := previewAction(t, preview)
	if action.State != "ready" || a.ResolveAction(action.Argv) != nil {
		t.Fatal(action)
	}
	applied := outputData(t, a, action.Argv[1:])
	if preview["candidate_digest"] != applied["candidate_digest"] || applied["changed"] != true {
		t.Fatal(preview, applied)
	}
	command := outputData(t, a, []string{"--isolated-root", root, "command", "inspect", "web"})["item"].(map[string]any)
	want := []any{}
	for _, v := range child {
		want = append(want, v)
	}
	if !reflect.DeepEqual(command["exec"], want) {
		t.Fatal("emitted apply changed the child command", command)
	}
}

func TestPreviewFileChangeRequiresNewReviewBeforeWrite(t *testing.T) {
	a := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	if e := os.WriteFile("devtools.toml", []byte("profile='app-a'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(root, "input.json")
	if e := os.WriteFile(file, []byte(`{"version":"24.x"}`), 0600); e != nil {
		t.Fatal(e)
	}
	preview := outputData(t, a, []string{"config", "tool", "add", "node", "--file", file, "--dry-run"})
	action := previewAction(t, preview)
	if action.State != "ready" {
		t.Fatal(action)
	}
	before, _ := os.ReadFile("devtools.toml")
	if e := os.WriteFile(file, []byte(`{"version":"26.x"}`), 0600); e != nil {
		t.Fatal(e)
	}
	code, out, diagnostic := invoke(t, a, "", action.Argv[1:]...)
	after, _ := os.ReadFile("devtools.toml")
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"preview_changed"`) || string(before) != string(after) {
		t.Fatal(code, out, diagnostic)
	}
}

func TestPreviewStdinHasAcquisitionStepWithoutBodyOrFakeApply(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	if e := os.WriteFile("devtools.toml", []byte("profile='app-a'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	code, out, diagnostic := invoke(t, a, `{"exec":["PRIVATE-INPUT-CANARY"]}`, "command", "add", "web", "--stdin", "--dry-run")
	if code != 0 {
		t.Fatal(out, diagnostic)
	}
	var result struct{ Data map[string]any }
	if e := json.Unmarshal([]byte(out), &result); e != nil {
		t.Fatal(e)
	}
	action := previewAction(t, result.Data)
	if action.State != "needs_input" || len(action.Argv) != 0 || len(action.Inputs) == 0 || len(action.InputSteps) == 0 || strings.Contains(out, "PRIVATE-INPUT-CANARY") || strings.Contains(out, "PROGRAM") {
		t.Fatal(out)
	}
	for _, step := range action.InputSteps {
		if step.State != "ready" || a.ResolveAction(step.Argv) != nil {
			t.Fatal(step)
		}
	}
	if _, e := os.Stat(filepath.Join(".", "private-input.json")); !os.IsNotExist(e) {
		t.Fatal("unexpected persisted input")
	}
}
