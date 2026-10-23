package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionGuidanceKeepsRootAndDirectTarget(t *testing.T) {
	a := testApp(t)
	projectDir := privateTempDir(t)
	t.Chdir(projectDir)
	if err := os.WriteFile("devtools.toml", []byte("profile='audit'\n[commands.missing]\nexec=['__missing_guidance_test__']\ninject=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(privateTempDir(t), "selected")
	code, _, diagnostic := invoke(t, a, "", "run", "--isolated-root", root, "missing")
	if code != 127 || !strings.Contains(diagnostic, root) {
		t.Fatal(code, diagnostic)
	}
	initializeTestVault(t, a)
	code, _, diagnostic = invoke(t, a, "", "run", "--", "__missing_guidance_test__")
	if code != 127 {
		t.Fatal(code, diagnostic)
	}
	var envelope struct {
		Error struct{ Details map[string]any }
	}
	if err := json.Unmarshal([]byte(diagnostic), &envelope); err != nil {
		t.Fatal(err)
	}
	d := envelope.Error.Details
	if d["target"].(map[string]any)["kind"] != "direct" {
		t.Fatal(d)
	}
	for _, r := range d["remedies"].([]any) {
		if strings.Contains(strings.Join(anyStrings(r.(map[string]any)["argv"]), " "), "doctor") {
			t.Fatal(d)
		}
	}
	if strings.Contains(diagnostic, `"commands","","exec"`) {
		t.Fatal(diagnostic)
	}
}

func anyStrings(v any) []string {
	out := []string{}
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
