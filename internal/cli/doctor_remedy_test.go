package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestDoctorStructuredRemedies(t *testing.T) {
	app := testApp(t)
	base := t.TempDir()
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
		Data struct {
			Checks []struct {
				ID       string            `json:"id"`
				Remedies []protocol.Remedy `json:"remedies"`
			} `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	byID := map[string][]protocol.Remedy{}
	for _, check := range envelope.Data.Checks {
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
	envelope.Data.Checks = nil
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	byID = map[string][]protocol.Remedy{}
	for _, check := range envelope.Data.Checks {
		byID[check.ID] = check.Remedies
	}
	command := byID["command"]
	if len(command) != 1 || !reflect.DeepEqual(command[0].Argv, []string{"devtools", "command", "list", "--dir", canonicalRoot}) {
		t.Fatalf("undefined command remedy lost diagnosed directory: %#v", command)
	}

	schema := outputData(t, app, []string{"schema", "doctor"})
	output := schema["output_schema"].(map[string]any)
	checks := output["properties"].(map[string]any)["checks"].(map[string]any)
	checkProps := checks["items"].(map[string]any)["properties"].(map[string]any)
	if _, legacy := checkProps["remedy"]; legacy {
		t.Fatalf("legacy remedy field remains in schema: %#v", checkProps)
	}
	remedies := checkProps["remedies"].(map[string]any)
	remedyProps := remedies["items"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"argv", "required_inputs", "message"} {
		if remedyProps[key] == nil {
			t.Fatalf("missing remedy field %s: %#v", key, remedyProps)
		}
	}
}
