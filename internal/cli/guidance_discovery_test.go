package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"io"
	"os"
	"strings"
	"testing"
)

func TestStatusSelectorsAgreeWithSchemaAndParser(t *testing.T) {
	a := testApp(t)
	entry := outputData(t, a, []string{"schema", "values", "execution", "status"})
	compiler := jsonschema.NewCompiler()
	if e := compiler.AddResource("urn:test:status", entry["input_schema"]); e != nil {
		t.Fatal(e)
	}
	schema, e := compiler.Compile("urn:test:status")
	if e != nil {
		t.Fatal(e)
	}
	var command Command
	for _, c := range a.commands {
		if c.Name == "values execution status" {
			command = c
		}
	}
	id := strings.Repeat("0", 32)
	requestID := "00000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		args  []string
		input map[string]any
		valid bool
	}{
		{[]string{}, map[string]any{"args": []any{}}, false},
		{[]string{id, "--request-id", requestID}, map[string]any{"args": []any{id}, "request-id": requestID}, false},
		{[]string{id}, map[string]any{"args": []any{id}}, true},
		{[]string{"--request-id", requestID}, map[string]any{"args": []any{}, "request-id": requestID}, true},
	} {
		_, e := parseRequest(command, tc.args)
		if (e == nil) != tc.valid || (schema.Validate(tc.input) == nil) != tc.valid {
			t.Fatal(tc, e, schema.Validate(tc.input))
		}
	}
}

func TestGeneratedHelpRemediesTerminateAtValidGroups(t *testing.T) {
	a := testApp(t)
	for _, args := range [][]string{{"help", "missing"}, {"help", "task", "missing"}, {"schema", "values", "missing"}} {
		code, _, diagnostic := invoke(t, a, "", args...)
		if code != 2 {
			t.Fatal(code, diagnostic)
		}
		var response struct {
			Error struct {
				Details struct{ Remedies []protocol.Remedy }
			}
		}
		if e := json.Unmarshal([]byte(diagnostic), &response); e != nil {
			t.Fatal(e)
		}
		for _, r := range response.Error.Details.Remedies {
			code, out, e := invoke(t, a, "", r.Argv[1:]...)
			if code != 0 || out == "" {
				t.Fatal(out, e)
			}
		}
	}
}

func TestConfiguredEnvInheritanceAndExplicitCommon(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	initializeTestVault(t, a)
	exe, _ := os.Executable()
	config := fmt.Sprintf("profile='audit'\n[commands.show]\nexec=[%q]\ninject=true\nenv='local'\n", exe)
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	outputData(t, a, []string{"env", "create", "local"})
	outputData(t, a, []string{"var", "set", "MODE", "--value", "common"})
	outputData(t, a, []string{"var", "set", "MODE", "--env", "local", "--value", "local"})
	ctx := process.WithRunner(context.Background(), func(_ context.Context, _ []string, _ string, env []string, _ io.Reader, out, _ io.Writer) (int, *protocol.Error) {
		for _, e := range env {
			if strings.HasPrefix(e, "MODE=") {
				fmt.Fprint(out, strings.TrimPrefix(e, "MODE="))
			}
		}
		return 0, nil
	})
	for _, tc := range []struct {
		args []string
		want string
	}{{[]string{"run", "show"}, "local"}, {[]string{"run", "--env", "", "show"}, "common"}, {[]string{"run", "--", exe}, "common"}} {
		var out, diagnostic bytes.Buffer
		if code := a.Run(ctx, tc.args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic}); code != 0 || out.String() != tc.want {
			t.Fatal(tc, code, out.String(), diagnostic.String())
		}
	}
}

func TestStatefulGenerateExampleUsesInspectedRevisionAndSameRequest(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	initializeTestVault(t, a)
	if e := os.WriteFile("devtools.toml", []byte("profile='audit'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	entry := outputData(t, a, []string{"schema", "secret", "generate"})
	if len(entry["workflow"].([]any)) == 0 {
		t.Fatal("stateful workflow absent")
	}
	revision := outputData(t, a, []string{"values", "revision"})["revision"].(string)
	argv := strings.Fields(entry["examples"].([]any)[0].(string))[1:]
	requestID := tasks.ID()
	for i := range argv {
		if argv[i] == "--revision" {
			argv[i+1] = revision
		}
		if argv[i] == "--request-id" {
			argv[i+1] = requestID
		}
	}
	first := outputData(t, a, argv)
	replay := outputData(t, a, argv)
	if first["changed"] != true || replay["replayed"] != true || first["revision"] != replay["revision"] {
		t.Fatal(first, replay)
	}
	_, help, e := invoke(t, a, "", "help", "command", "run")
	if e != "" || !strings.Contains(help, "inherits the saved command env") || !strings.Contains(help, "choices: auto, redacted, raw") {
		t.Fatal(help, e)
	}
}
