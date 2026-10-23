package cli

import (
	"encoding/json"
	"fmt"
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

func TestVaultAndExplainGuidanceKeepExplicitRoot(t *testing.T) {
	a := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile("devtools.toml", []byte("profile='audit'\n[commands.show]\nexec=['node']\ninject=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(privateTempDir(t), "B")
	code, _, diagnostic := invoke(t, a, "synthetic", "sec", "set", "TOKEN", "--stdin", "--isolated-root", root)
	if code != 3 || !strings.Contains(diagnostic, root) {
		t.Fatal(code, diagnostic)
	}
	var failure struct {
		Error struct {
			Details struct{ Remedies []struct{ Argv []string } }
		}
	}
	if err := json.Unmarshal([]byte(diagnostic), &failure); err != nil {
		t.Fatal(err)
	}
	for _, r := range failure.Error.Details.Remedies {
		code, out, err := invoke(t, a, "", r.Argv[1:]...)
		if code != 0 || !strings.Contains(out, `"uninitialized"`) {
			t.Fatal(out, err)
		}
	}
	code, out, err := invoke(t, a, "", "command", "explain", "show", "--isolated-root", root)
	if code != 0 || !strings.Contains(out, root) {
		t.Fatal(out, err)
	}
}

func TestPreparationEffectsMatchNewProfilePublication(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	initializeTestVault(t, a)
	code, _, diagnostic := invoke(t, a, "", "run", "--profile", "invalid-new", "--env", "missing", "--", "__missing_effect_test__")
	if code == 0 || !strings.Contains(diagnostic, `"effects":"none"`) {
		t.Fatal(code, diagnostic)
	}
	code, _, diagnostic = invoke(t, a, "", "run", "--profile", "valid-new", "--", "__missing_effect_test__")
	if code != 127 || !strings.Contains(diagnostic, `"effects":"partial"`) {
		t.Fatal(code, diagnostic)
	}
}

func TestWrongKindAndLockedRunKeepActionableConditions(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	initializeTestVault(t, a)
	exe, _ := os.Executable()
	config := fmt.Sprintf("profile='audit'\n[commands.need]\nexec=[%q]\ninject=true\n[commands.need.requirements]\nsecs=['MISMATCH']\n", exe)
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, e := invoke(t, a, "", "var", "set", "MISMATCH", "--value", "public"); code != 0 {
		t.Fatal(e)
	}
	code, _, e := invoke(t, a, "", "run", "need")
	if code != 3 || !strings.Contains(e, "kind_mismatch") || strings.Contains(e, `"sec","set","MISMATCH"`) {
		t.Fatal(code, e)
	}
	if code, _, e := invoke(t, a, "", "vault", "lock"); code != 0 {
		t.Fatal(e)
	}
	code, _, e = invoke(t, a, "", "run", "need")
	if code != 3 || !strings.Contains(e, `"--passphrase-stdin"`) || !strings.Contains(e, `"required_inputs":["stdin"]`) || !strings.Contains(e, `"retry":"after_dependency_change"`) {
		t.Fatal(code, e)
	}
}
