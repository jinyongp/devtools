package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestProxyPresenceContractBeforeProjectAndStoreAccess(t *testing.T) {
	t.Chdir(privateTempDir(t))
	app := testApp(t)
	for _, command := range []string{"command run", "run", "cmd run", "process start", "project up"} {
		data := outputData(t, app, append([]string{"schema"}, strings.Fields(command)...))
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("urn:proxy-presence", data["input_schema"]); err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile("urn:proxy-presence")
		if err != nil {
			t.Fatal(err)
		}
		for _, proxy := range []string{"", "false", "true"} {
			for _, noProxy := range []string{"", "false", "true"} {
				input := map[string]any{"args": []any{"serve"}}
				args := strings.Fields(command)
				for _, pair := range [][2]string{{"proxy", proxy}, {"no-proxy", noProxy}} {
					if pair[1] != "" {
						input[pair[0]] = pair[1] == "true"
						args = append(args, "--"+pair[0]+"="+pair[1])
					}
				}
				if command == "process start" || command == "project up" {
					input["request-id"] = "00000000-0000-4000-8000-000000000001"
					args = append(args, "--request-id", input["request-id"].(string))
				}
				args = append(args, "serve")
				valid := proxy == "" || noProxy == ""
				if (schema.Validate(input) == nil) != valid {
					t.Fatal(command, input, "schema presence mismatch")
				}
				code, _, diagnostic := invoke(t, app, "", args...)
				if valid {
					if code != 3 || !strings.Contains(diagnostic, "project_not_found") {
						t.Fatal(command, input, code, diagnostic)
					}
				} else {
					if code != 2 || !strings.Contains(diagnostic, `"rule_id"`) || !strings.Contains(diagnostic, "no-proxy") {
						t.Fatal(command, input, code, diagnostic)
					}
				}
			}
		}
	}
	app.dataDirectory = func() (string, *protocol.Error) { t.Fatal("static failure accessed task/value store"); return "", nil }
	if code, _, err := invoke(t, app, "", "project", "up", "--proxy=false", "--no-proxy=false", "--request-id", "00000000-0000-4000-8000-000000000001", "serve"); code != 2 {
		t.Fatal(code, err)
	}
}

func TestStaticRuleDiagnosticsAndHelpShareActionableConditions(t *testing.T) {
	app := testApp(t)
	for _, tc := range []struct {
		command string
		flags   []string
		message string
	}{
		{"vault rewrap", []string{"--from-backend", "passphrase", "--backend", "passphrase", "--current-passphrase-stdin", "--new-passphrase-stdin", "--request-id", "00000000-0000-4000-8000-000000000001"}, "--current-passphrase-stdin and --new-passphrase-stdin cannot both be true."},
		{"proxy teardown", []string{"--apply", strings.Repeat("a", 64)}, "--apply and --request-id must be provided together."},
		{"proxy setup", nil, "Setup changes require --request-id; use --check for inspection."},
		{"vault init", []string{"--backend", "native", "--passphrase-file", "missing"}, "Passphrase input requires --backend=passphrase."},
	} {
		args := append(strings.Fields(tc.command), tc.flags...)
		reader := &observedSecretReader{}
		var out, diagnostic strings.Builder
		code := app.Run(t.Context(), args, IO{In: reader, Out: &out, Err: &diagnostic})
		var env map[string]any
		if code != 2 || reader.read || json.Unmarshal([]byte(diagnostic.String()), &env) != nil {
			t.Fatal(tc.command, code, diagnostic.String())
		}
		e := env["error"].(map[string]any)
		if e["message"] != tc.message {
			t.Fatal(e)
		}
		facts := e["details"].(map[string]any)["facts"].(map[string]any)
		if facts["rule_id"] == nil || len(facts["options"].([]any)) < 1 {
			t.Fatal(facts)
		}
		_, help, err := invoke(t, app, "", append(strings.Fields(tc.command), "--help")...)
		if err != "" || !strings.Contains(help, tc.message) {
			t.Fatal(fmt.Sprint(help, err))
		}
	}
}
