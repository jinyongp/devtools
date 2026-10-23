package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestExpiredDoctorInputRetainsAuthenticatedPrerequisite(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	initializeTestVault(t, a)
	if e := os.WriteFile("devtools.toml", []byte("profile='audit'\n[commands.check]\nexec=['__missing_guidance__']\ninject=true\nsecs=['TOKEN']\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if code, _, diagnostic := invoke(t, a, "CANARY_EXPIRED_VALUE", "secret", "set", "TOKEN", "--stdin"); code != 0 {
		t.Fatal(code, diagnostic)
	}
	store, failure := a.store(map[string]string{})
	if failure != nil {
		t.Fatal(failure)
	}
	ctx := context.Background()
	master, e := store.Master(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer master.Destroy()
	root := filepath.Dir(store.Directory)
	slot, e := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Read()
	if e != nil {
		t.Fatal(e)
	}
	files := vault.FileStore{Root: root, VaultID: slot.VaultID, CatalogKey: master}
	_, key, e := files.ReadWithMaster(ctx, "audit", master)
	if e != nil {
		t.Fatal(e)
	}
	defer key.Destroy()
	expired := time.Now().Add(-time.Hour).Unix()
	if _, e := files.Update(ctx, "audit", key, func(p *vault.Profile) (bool, error) {
		p.Values[0].Slots[0].ExpiresAt = &expired
		p.Revision++
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	before := outputData(t, a, []string{"vault", "status"})["item"].(map[string]any)["idle_expires_at"]
	if before == nil {
		t.Fatal("fixture has no reusable session idle deadline")
	}
	code, out, diagnostic := invoke(t, a, "", "doctor", "check")
	if code != 0 || strings.Contains(out, "CANARY_EXPIRED_VALUE") {
		t.Fatal(code, diagnostic)
	}
	var report struct {
		Data struct {
			Commands []struct {
				Checks []struct {
					ID, Reason string
					Facts      map[string]any
				}
			}
		}
	}
	if e := json.Unmarshal([]byte(out), &report); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, check := range report.Data.Commands[0].Checks {
		if check.ID == "sec:TOKEN" {
			found = check.Reason == "value_expired" && check.Facts["actual_kind"] == "secret"
		}
	}
	if !found {
		t.Fatal("authenticated expiry was mistaken for unverified registration", out)
	}
	after := outputData(t, a, []string{"vault", "status"})["item"].(map[string]any)["idle_expires_at"]
	if before != after {
		t.Fatal("doctor extended the secret session idle deadline")
	}
}

func TestExecutionGuidanceKeepsRootAndDirectTarget(t *testing.T) {
	a := testApp(t)
	projectDir := privateTempDir(t)
	t.Chdir(projectDir)
	if err := os.WriteFile("devtools.toml", []byte("profile='audit'\n[commands.missing]\nexec=['__missing_guidance_test__']\ninject=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(privateTempDir(t), "selected")
	rootJSON, _ := json.Marshal(root)
	code, _, diagnostic := invoke(t, a, "", "run", "--isolated-root", root, "missing")
	if code != 127 || !strings.Contains(diagnostic, string(rootJSON)) {
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
	for _, args := range [][]string{
		{"run", "--", "__missing_guidance_test__"},
		{"command", "run", "--", "__missing_guidance_test__"},
		{"--isolated-root", root, "run", "--profile", "audit", "--", "__missing_guidance_test__"},
	} {
		code, _, diagnostic = invoke(t, a, "", args...)
		if code != 127 {
			t.Fatal(code, diagnostic)
		}
		var response struct {
			Error struct {
				Details struct {
					Checks []struct {
						ID      string
						Sources []struct {
							Kind   string
							Offset *int
						}
					}
				}
			}
		}
		if e := json.Unmarshal([]byte(diagnostic), &response); e != nil {
			t.Fatal(e)
		}
		found := false
		for _, check := range response.Error.Details.Checks {
			if check.ID == "command_executable" {
				found = len(check.Sources) == 1 && check.Sources[0].Kind == "argv" && check.Sources[0].Offset != nil && *check.Sources[0].Offset == len(args)-1
			}
		}
		if !found {
			t.Fatal("direct executable lost its original argv position", diagnostic)
		}
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
	rootJSON, _ := json.Marshal(root)
	code, _, diagnostic := invoke(t, a, "synthetic", "sec", "set", "TOKEN", "--stdin", "--isolated-root", root)
	if code != 3 || !strings.Contains(diagnostic, string(rootJSON)) {
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
	if code != 0 || !strings.Contains(out, string(rootJSON)) {
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
	code, _, e = invoke(t, a, "", "values", "execution", "status", strings.Repeat("0", 32))
	if code != 3 || !strings.Contains(e, `"--passphrase-stdin"`) || !strings.Contains(e, `"required_inputs":["stdin"]`) || !strings.Contains(e, `"retry":"after_dependency_change"`) {
		t.Fatal("external execution inspection lost the vault prerequisite", code, e)
	}
}

func TestImportAndConfigDiagnosticsKeepSafeFacts(t *testing.T) {
	a := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile("devtools.toml", []byte("profile='audit'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, e := invoke(t, a, "", "var", "set", "TOKEN", "--value", "old"); code != 0 {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "input.env")
	if err := os.WriteFile(file, []byte("TOKEN=CANARY_DIAGNOSTIC_VALUE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, e := invoke(t, a, "", "import", "--file", file)
	if code != 3 || !strings.Contains(e, `"import_items":[{"action":"conflict","key":"TOKEN","kind":"variable"}]`) || strings.Contains(e, "CANARY_DIAGNOSTIC_VALUE") {
		t.Fatal(code, e)
	}
	if err := os.WriteFile(file, []byte("OK=public\nTOKEN=\"CANARY_UNCLOSED\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, e = invoke(t, a, "", "import", "--file", file)
	if code != 2 || !strings.Contains(e, `"kind":"dotenv","line":2`) || strings.Contains(e, "CANARY_UNCLOSED") {
		t.Fatal(code, e)
	}
	if err := os.WriteFile("devtools.toml", []byte("profile='audit'\n[commands.dev]\nexec=['missing']\ninject='CANARY_WRONG_TYPE'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, e = invoke(t, a, "", "config", "check")
	if code != 3 || !strings.Contains(e, `"expected_type":"boolean"`) || !strings.Contains(e, `"observed_type":"string"`) || strings.Contains(e, "CANARY_WRONG_TYPE") || strings.Contains(e, "Run devtools config check") {
		t.Fatal(code, e)
	}
}

func TestLegacyConfigTypeFactsMatchRegistry(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	for _, suffix := range []string{"", "[commands.dev.requirements]\nsecs=['TOKEN']\n"} {
		config := "profile='audit'\n[commands.dev]\nexec=['missing']\ninject='CANARY_WRONG_TYPE'\n" + suffix
		if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
			t.Fatal(e)
		}
		code, _, diagnostic := invoke(t, a, "", "config", "check")
		if code != 3 || !strings.Contains(diagnostic, `"expected_type":"boolean"`) || !strings.Contains(diagnostic, `"observed_type":"string"`) || strings.Contains(diagnostic, "CANARY_WRONG_TYPE") {
			t.Fatal(code, diagnostic)
		}
	}
}
