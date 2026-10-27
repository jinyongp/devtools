package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestDoctorStructuredRemedies(t *testing.T) {
	app := testApp(t)
	base := privateTempDir(t)
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "project-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	config := `profile="app"
[requirements]
vars=["PORT"]
[commands.check]
exec=["/bin/sh","-c","true"]
env="dev"
[commands.check.requirements]
secs=["TOKEN"]
`
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := invoke(t, app, "", "env", "create", "dev"); code != 0 {
		t.Fatal(diagnostic)
	}

	code, out, diagnostic := invoke(t, app, "", "doctor", "check")
	if code != 0 || diagnostic != "" {
		t.Fatalf("doctor failed: code=%d out=%s err=%s", code, out, diagnostic)
	}
	var envelope struct {
		Data doctor.Diagnosis `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	byID := map[string][]protocol.Remedy{}
	for _, check := range envelope.Data.Commands[0].Checks {
		byID[check.ID] = check.Remedies
	}
	variable := byID["var:PORT"]
	if len(variable) != 1 || !reflect.DeepEqual(variable[0].Argv, []string{"devtools", "var", "set", "PORT", "--profile", "app", "--env", "dev"}) || !reflect.DeepEqual(variable[0].RequiredInputs, []string{"value"}) {
		t.Fatalf("unexpected variable remedy: %#v", variable)
	}
	secret := byID["sec:TOKEN"]
	if len(secret) != 1 || !reflect.DeepEqual(secret[0].Argv, []string{"devtools", "sec", "set", "TOKEN", "--profile", "app", "--env", "dev", "--stdin"}) || !reflect.DeepEqual(secret[0].RequiredInputs, []string{"stdin"}) {
		t.Fatalf("unexpected secret remedy: %#v", secret)
	}

	code, out, diagnostic = invoke(t, app, "", "doctor", "missing", "--dir", alias)
	if code != 0 || diagnostic != "" {
		t.Fatalf("undefined-command doctor failed: code=%d out=%s err=%s", code, out, diagnostic)
	}
	envelope.Data.Commands = nil
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	byID = map[string][]protocol.Remedy{}
	for _, check := range envelope.Data.Commands[0].Checks {
		byID[check.ID] = check.Remedies
	}
	command := byID["command"]
	if len(command) != 1 || !reflect.DeepEqual(command[0].Argv, []string{"devtools", "command", "list", "--dir", canonicalRoot}) {
		t.Fatalf("undefined command remedy lost diagnosed directory: %#v", command)
	}

	schema := outputData(t, app, []string{"schema", "doctor"})
	output := schema["output_schema"].(map[string]any)
	defs := output["$defs"].(map[string]any)
	checkProps := defs["Check"].(map[string]any)["properties"].(map[string]any)
	if _, legacy := checkProps["remedy"]; legacy {
		t.Fatalf("legacy remedy field remains in schema: %#v", checkProps)
	}
	remedyProps := defs["Remedy"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"argv", "required_inputs", "message", "effect"} {
		if remedyProps[key] == nil {
			t.Fatalf("missing remedy field %s: %#v", key, remedyProps)
		}
	}
}

func TestDoctorDeadlineRecoverySurvivesReportAndText(t *testing.T) {
	app := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	probe := commandFixture(t, "hold")
	args, _ := json.Marshal(probe[1:])
	config := fmt.Sprintf("profile=\"app\"\n[tools.slow]\nexecutable=%s\nversion=\"1.2.3\"\nversion_args=%s\n[defaults]\ntools=[\"slow\"]\n[commands.check]\nexec=%s\ninject=false\n", mustJSON(t, probe[0]), args, fixtureExec(t, "true"))
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, check := range []bool{false, true} {
		argv := []string{"doctor", "check", "--timeout", "50ms"}
		if check {
			argv = append(argv, "--check")
		}
		code, out, diagnostic := invoke(t, app, "", argv...)
		var envelope map[string]any
		var report map[string]any
		if check {
			if code != 3 || json.Unmarshal([]byte(diagnostic), &envelope) != nil {
				t.Fatal(code, diagnostic)
			}
			assertContract(t, "Failure", envelope)
			report = envelope["error"].(map[string]any)["details"].(map[string]any)["report"].(map[string]any)
		} else {
			if code != 0 || json.Unmarshal([]byte(out), &envelope) != nil {
				t.Fatal(code, out, diagnostic)
			}
			report = envelope["data"].(map[string]any)
		}
		if report["ready"] != false {
			t.Fatal(report)
		}
		found := false
		for _, value := range report["commands"].([]any)[0].(map[string]any)["checks"].([]any) {
			c := value.(map[string]any)
			if c["id"] == "tool:slow" {
				remedy := c["remedies"].([]any)[0].(map[string]any)
				found = c["status"] == "unknown" && c["reason"] == "deadline_exceeded" && remedy["effect"] == "read" && strings.Contains(remedy["message"].(string), "overall timeout")
			}
		}
		if !found {
			t.Fatal(report)
		}
		code, out, diagnostic = invoke(t, app, "", append([]string{"--format", "text"}, argv...)...)
		if code != map[bool]int{false: 0, true: 3}[check] || !strings.Contains(out+diagnostic, "overall timeout") || strings.Contains(out+diagnostic, "system_write") {
			t.Fatal(code, out, diagnostic)
		}
	}
}
