package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jinyongp/devtools/internal/configcontract"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func assertContract(t *testing.T, name string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	value, issue := configcontract.Decode(b)
	if issue != nil {
		t.Fatal(issue)
	}
	if issues := configcontract.Validate(name, value); len(issues) > 0 {
		t.Fatalf("%s: %+v\n%s", name, issues, b)
	}
}

func TestEveryPublishedBodyExampleMatchesItsSchema(t *testing.T) {
	app := testApp(t)
	for i, command := range app.commands {
		if command.BodySchema == nil {
			continue
		}
		t.Run(command.Name, func(t *testing.T) {
			data, err := json.Marshal(command.BodySchema)
			if err != nil {
				t.Fatal(err)
			}
			var schema any
			if err := json.Unmarshal(data, &schema); err != nil {
				t.Fatal(err)
			}
			compiler := jsonschema.NewCompiler()
			compiler.AssertFormat()
			url := fmt.Sprintf("urn:devtools:test:body:%d", i)
			if err := compiler.AddResource(url, schema); err != nil {
				t.Fatal(err)
			}
			compiled, err := compiler.Compile(url)
			if err != nil {
				t.Fatal(err)
			}
			if err := compiled.Validate(command.BodyExample); err != nil {
				t.Fatalf("%v: %#v", err, command.BodyExample)
			}
		})
	}
}

func TestEveryPublishedInvocationExampleParses(t *testing.T) {
	app := testApp(t)
	catalog := app.catalog()["commands"].([]map[string]any)
	for index, command := range app.commands {
		for _, example := range command.Examples {
			t.Run(command.Name, func(t *testing.T) {
				// Generated examples contain separate, unquoted synthetic tokens.
				tokens := strings.Fields(example)
				var rest []string
				for _, name := range append([]string{command.Name}, command.Aliases...) {
					prefix := append([]string{"devtools"}, strings.Fields(name)...)
					if len(tokens) >= len(prefix) && reflect.DeepEqual(tokens[:len(prefix)], prefix) {
						rest = tokens[len(prefix):]
						break
					}
				}
				if rest == nil {
					t.Fatal("invocation names a different command", example)
				}
				request, err := parseRequest(command, rest)
				if err != nil {
					t.Fatal(example, err)
				}
				input := map[string]any{"args": request.Args}
				if request.Child != nil {
					input["child_args"] = request.Child
				}
				for _, option := range command.Options {
					if value, ok := request.Options[option.Name]; ok {
						switch {
						case option.Boolean:
							input[option.Name] = value == "true"
						case option.Repeatable:
							input[option.Name] = request.ListOptions[option.Name]
						default:
							input[option.Name] = value
						}
					}
				}
				data, _ := json.Marshal(catalog[index]["input_schema"])
				var schema any
				_ = json.Unmarshal(data, &schema)
				compiler := jsonschema.NewCompiler()
				const url = "urn:devtools:test:invocation"
				if err := compiler.AddResource(url, schema); err != nil {
					t.Fatal(err)
				}
				compiled, compileErr := compiler.Compile(url)
				if compileErr != nil {
					t.Fatal(compileErr)
				}
				data, _ = json.Marshal(input)
				var normalized any
				_ = json.Unmarshal(data, &normalized)
				if err := compiled.Validate(normalized); err != nil {
					t.Fatal(example, err)
				}
			})
		}
	}
}

func TestResponseSchemaResolvesNestedDiagnosticDefinitions(t *testing.T) {
	app := testApp(t)
	catalog := app.catalog()
	data, _ := json.Marshal(catalog["response_schema"])
	var schema any
	_ = json.Unmarshal(data, &schema)
	compiler := jsonschema.NewCompiler()
	const url = "urn:devtools:test:response"
	if err := compiler.AddResource(url, schema); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	code, _, diagnostic := invoke(t, app, "", "dotor")
	if code != 2 {
		t.Fatal(code, diagnostic)
	}
	var response any
	_ = json.Unmarshal([]byte(diagnostic), &response)
	if err := compiled.Validate(response); err != nil {
		t.Fatal(err)
	}
}
func TestConfigCLIJourney(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	if e := os.WriteFile("devtools.toml", []byte("# sample\nprofile='sample'\n"), 0640); e != nil {
		t.Fatal(e)
	}
	initial := outputData(t, app, []string{"config", "inspect"})
	assertContract(t, "InspectResult", initial)
	revision := initial["revision"].(string)
	body := `{"operations":[{"op":"tool.add","name":"node","value":{}},{"op":"preset.add","name":"web","value":{"tools":["node"]}},{"op":"command.add","name":"dev.web","value":{"exec":["node","scripts/dev.mjs"],"requires":["web"]}}]}`
	code, out, diagnostic := invoke(t, app, body, "config", "edit", "--stdin", "--revision", revision, "--dry-run")
	if code != 0 || diagnostic != "" {
		t.Fatal(code, out, diagnostic)
	}
	var preview struct{ Data map[string]any }
	_ = json.Unmarshal([]byte(out), &preview)
	assertContract(t, "EditResult", preview.Data)
	code, out, diagnostic = invoke(t, app, body, "config", "edit", "--stdin", "--revision", revision)
	if code != 0 || diagnostic != "" {
		t.Fatal(code, out, diagnostic)
	}
	var applied struct{ Data map[string]any }
	_ = json.Unmarshal([]byte(out), &applied)
	assertContract(t, "EditResult", applied.Data)
	if preview.Data["candidate_digest"] != applied.Data["candidate_digest"] {
		t.Fatal("dry-run/apply diverged")
	}
	assertContract(t, "ValidationReport", outputData(t, app, []string{"config", "check"}))
	assertContract(t, "ExplainResult", outputData(t, app, []string{"command", "explain", "dev.web"}))
	assertContract(t, "DoctorReport", outputData(t, app, []string{"doctor", "dev.web", "dev.web"}))
	code, out, diagnostic = invoke(t, app, `{"version":"2.0.0"}`, "config", "tool", "update", "node", "--stdin", "--revision", revision)
	if code != 3 || out != "" {
		t.Fatal(code, out, diagnostic)
	}
	var failed map[string]any
	_ = json.Unmarshal([]byte(diagnostic), &failed)
	assertContract(t, "Failure", failed)
	if failed["error"].(map[string]any)["code"] != "config_revision_conflict" {
		t.Fatal(diagnostic)
	}
}
func TestConfigInputErrorsAreSafeAndActionable(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	_ = os.WriteFile("devtools.toml", []byte("profile='sample'\n"), 0600)
	rev := outputData(t, app, []string{"config", "inspect"})["revision"].(string)
	for _, body := range []string{`{"version":"CANARY-PRIVATE","version":"1.2.3"}`, `{"unexpected":"CANARY-PRIVATE"}`, `{"version_args":["\ud800"]}`} {
		code, out, diagnostic := invoke(t, app, body, "config", "tool", "add", "node", "--stdin", "--revision", rev)
		if code != 2 || out != "" || strings.Contains(diagnostic, "CANARY-PRIVATE") {
			t.Fatal(code, out, diagnostic)
		}
		var failed map[string]any
		_ = json.Unmarshal([]byte(diagnostic), &failed)
		assertContract(t, "Failure", failed)
	}
	code, out, diagnostic := invoke(t, app, "", "dotor")
	if code != 2 || out != "" || !strings.Contains(diagnostic, `"suggestions":["doctor"]`) {
		t.Fatal(code, out, diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "", "doctor", "--bndle", "all")
	if code != 2 || out != "" || !strings.Contains(diagnostic, `"suggestions":["bundle"]`) {
		t.Fatal(code, out, diagnostic)
	}
	if data, e := os.ReadFile(filepath.Join(root, "devtools.toml")); e != nil || string(data) != "profile='sample'\n" {
		t.Fatal("invalid request changed file")
	}
}

func TestInputReadFailureKeepsRoleWithoutPrivatePath(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	root := privateTempDir(t)
	t.Chdir(root)
	_ = os.WriteFile("devtools.toml", []byte("profile='sample'\n"), 0600)
	for _, args := range [][]string{
		{"sec", "set", "TOKEN", "--file", "PRIVATE-MISSING-FILE"},
		{"profile", "import", "--file", "PRIVATE-MISSING-FILE"},
	} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 1 || out != "" || strings.Contains(diagnostic, "PRIVATE-MISSING-FILE") {
			t.Fatal(code, out, diagnostic)
		}
		var response map[string]any
		_ = json.Unmarshal([]byte(diagnostic), &response)
		assertContract(t, "Failure", response)
		details := response["error"].(map[string]any)["details"].(map[string]any)
		if details["reason"] != "not_found" || details["facts"].(map[string]any)["input_role"] == nil {
			t.Fatal(details)
		}
	}
}

func TestConfigInputDeadlineDoesNotWaitForPipeEOF(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	before := []byte("profile='sample'\n")
	_ = os.WriteFile("devtools.toml", before, 0600)
	revision := outputData(t, app, []string{"config", "inspect"})["revision"].(string)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var out, diagnostic bytes.Buffer
	start := time.Now()
	code := app.Run(ctx, []string{"config", "edit", "--revision", revision, "--stdin"}, IO{In: reader, Out: &out, Err: &diagnostic})
	if code != 3 || time.Since(start) > time.Second || !strings.Contains(diagnostic.String(), "deadline_exceeded") {
		t.Fatal(code, diagnostic.String())
	}
	data, _ := os.ReadFile("devtools.toml")
	if string(data) != string(before) {
		t.Fatal("canceled input changed configuration")
	}
}
func TestConfigSchemaAndHelpShareBodyExamples(t *testing.T) {
	app := testApp(t)
	for _, args := range [][]string{{"config", "edit"}, {"config", "tool", "add"}, {"config", "tool", "update"}, {"config", "preset", "add"}, {"config", "preset", "update"}, {"command", "add"}, {"command", "update"}} {
		entry := outputData(t, app, append([]string{"schema"}, args...))
		if entry["body_example"] == nil {
			t.Fatalf("missing body example: %v", args)
		}
		body := entry["body_schema"].(map[string]any)
		definition := strings.TrimPrefix(body["$ref"].(string), "#/$defs/")
		assertContract(t, definition, entry["body_example"])
	}
}

func TestExplainDiagnosisRemedyPreservesSelectionOverrides(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	if err := os.WriteFile("devtools.toml", []byte("profile='sample'\n[commands.check]\nexec=['node']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data := outputData(t, app, []string{"command", "explain", "check", "--profile", "other", "--env", "testing", "--bundle", "none"})
	unresolved := data["unresolved"].([]any)
	if len(unresolved) != 1 {
		t.Fatal(unresolved)
	}
	remedies := unresolved[0].(map[string]any)["remedies"].([]any)
	argv := remedies[0].(map[string]any)["argv"]
	want := []any{"devtools", "doctor", "--dir", root, "--profile", "other", "--bundle", "none", "--mode", "foreground", "--env", "testing", "check"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatal("diagnosis would inspect a different selection", argv)
	}
}
