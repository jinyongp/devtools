package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestExecutionSourceContractsBeforeDependencies(t *testing.T) {
	const uuid = "00000000-0000-4000-8000-000000000001"
	const id = "00000000000000000000000000000001"
	for _, tc := range []struct {
		command, body string
		argument      bool
	}{
		{"values execution prepare", `{"external_execution_id":"fixture","selection":{"keys":[]}}`, false},
		{"values execution deliver", `{"revision":1}`, true},
		{"values execution report", `{"revision":1,"runner_id":"` + uuid + `","external_execution_id":"fixture","input_id":"` + id + `","state":"unknown","reason":"outcome_unavailable"}`, true},
		{"values execution approve", `{"revision":1,"runner_id":"` + uuid + `","runner_instance_id":"` + uuid + `","external_execution_id":"fixture","input_id":"` + id + `"}`, true},
		{"values execution cancel", `{"revision":1}`, true},
		{"values runner register", `{"runner_id":"` + uuid + `","instance_id":"` + uuid + `","protocol":1,"redaction_protocol":1}`, false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			app := testApp(t)
			file := filepath.Join(privateTempDir(t), "body.json")
			if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			data := outputData(t, app, append([]string{"schema"}, strings.Fields(tc.command)...))
			compiler := jsonschema.NewCompiler()
			if err := compiler.AddResource("urn:source", data["input_schema"]); err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile("urn:source")
			if err != nil {
				t.Fatal(err)
			}
			accessed := false
			app.dataDirectory = func() (string, *protocol.Error) {
				accessed = true
				return "", protocol.NewError("test_store_boundary", "Stopped at dependency boundary.", 3, nil)
			}
			for _, row := range []struct {
				input map[string]any
				flags []string
				valid bool
			}{
				{map[string]any{}, nil, false},
				{map[string]any{"stdin": false}, []string{"--stdin=false"}, false},
				{map[string]any{"file": ""}, []string{"--file="}, false},
				{map[string]any{"file": "", "stdin": true}, []string{"--file=", "--stdin"}, false},
				{map[string]any{"file": file, "stdin": true}, []string{"--file", file, "--stdin"}, false},
				{map[string]any{"file": file}, []string{"--file", file}, true},
				{map[string]any{"file": file, "stdin": false}, []string{"--file", file, "--stdin=false"}, true},
				{map[string]any{"stdin": true}, []string{"--stdin"}, true},
			} {
				input := map[string]any{"request-id": uuid}
				for k, v := range row.input {
					input[k] = v
				}
				args := append(strings.Fields(tc.command), "--request-id", uuid)
				if tc.argument {
					input["args"] = []any{id}
					args = append(args, id)
				}
				args = append(args, row.flags...)
				if (schema.Validate(input) == nil) != row.valid {
					t.Fatal(input, "schema mismatch")
				}
				accessed = false
				code, _, diagnostic := invoke(t, app, tc.body, args...)
				if row.valid {
					if code != 3 || !accessed || !strings.Contains(diagnostic, "test_store_boundary") {
						t.Fatal(args, code, diagnostic)
					}
				} else if code != 2 || accessed {
					t.Fatal(args, code, diagnostic, "accessed", accessed)
				}
			}
		})
	}
}

func TestEmptyTaskFileDoesNotConsumeStdinOrCreateWork(t *testing.T) {
	app := testApp(t)
	reader := &observedSecretReader{}
	var out, diagnostic strings.Builder
	code := app.Run(t.Context(), []string{"task", "workstream", "create", "--file=", "--stdin", "--request-id", "00000000-0000-4000-8000-000000000001"}, IO{In: reader, Out: &out, Err: &diagnostic})
	if code != 2 || reader.read {
		t.Fatal(code, diagnostic.String())
	}
	var envelope map[string]any
	if json.Unmarshal([]byte(diagnostic.String()), &envelope) != nil {
		t.Fatal(diagnostic.String())
	}
	assertContract(t, "Failure", envelope)
}

func TestProfileTransferConflictsDoNotTreatFalseAsAnInactivePath(t *testing.T) {
	app := testApp(t)
	for _, args := range [][]string{
		{"profile", "export", "--recipient-file", "false", "--passphrase-stdin"},
		{"profile", "import", "--file", "unused.age", "--identity-file", "false", "--passphrase-stdin"},
	} {
		reader := &observedSecretReader{}
		var out, diagnostic strings.Builder
		if code := app.Run(t.Context(), args, IO{In: reader, Out: &out, Err: &diagnostic}); code != 2 || reader.read {
			t.Fatal(code, diagnostic.String())
		}
	}
	if passphraseOptionConflict(map[string]string{"recipient-file": "false", "passphrase-stdin": "true"}, "recipient-file") == nil {
		t.Fatal("filename false was mistaken for boolean false")
	}
}
