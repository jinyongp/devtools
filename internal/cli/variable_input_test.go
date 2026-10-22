package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestVariableInputPreservesValuesWithoutVault(t *testing.T) {
	file := filepath.Join(privateTempDir(t), "value")
	for _, value := range []struct{ name, text string }{{"empty", ""}, {"multiline", "  첫 줄\r\n$NAME\n\n"}} {
		if err := os.WriteFile(file, []byte(value.text), 0600); err != nil {
			t.Fatal(err)
		}
		for _, input := range []struct {
			name, command string
			flags         []string
		}{
			{"literal", "var", []string{"--value", value.text}},
			{"stdin", "var", []string{"--stdin"}},
			{"file", "variable", []string{"--file", file}},
		} {
			t.Run(value.name+"/"+input.name, func(t *testing.T) {
				app := testApp(t)
				args := append([]string{input.command, "set", "TEXT", "--profile", "app"}, input.flags...)
				for _, changed := range []bool{true, false} {
					code, out, diagnostic := invoke(t, app, value.text, args...)
					var envelope struct {
						Data struct{ Changed bool }
					}
					if code != 0 || json.Unmarshal([]byte(out), &envelope) != nil || envelope.Data.Changed != changed {
						t.Fatalf("changed=%t: code=%d out=%s err=%s", changed, code, out, diagnostic)
					}
					got := outputData(t, app, []string{"var", "get", "TEXT", "--profile", "app"})
					if got["value"] != value.text {
						t.Fatalf("value=%q want=%q", got["value"], value.text)
					}
				}
			})
		}
	}
}

func TestVariableInputCreatesEnvironmentOverride(t *testing.T) {
	const value = "  local\r\n$NAME\n"
	file := filepath.Join(privateTempDir(t), "value")
	if err := os.WriteFile(file, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"stdin", "file"} {
		for _, mode := range []string{"legacy", "guarded"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				app := testApp(t)
				outputData(t, app, []string{"var", "set", "TEXT", "--profile", "app", "--value", "common"})
				outputData(t, app, []string{"env", "create", "local", "--profile", "app"})
				args := []string{"var", "set", "TEXT", "--profile", "app", "--env", "local", "--" + source}
				if source == "file" {
					args = append(args, file)
				}
				if mode == "guarded" {
					args = append(args, "--revision", editToken(t, app), "--request-id", tasks.ID(), "--create-only")
				}
				if code, _, diagnostic := invoke(t, app, value, args...); code != 0 {
					t.Fatal(code, diagnostic)
				}
				if got := outputData(t, app, []string{"var", "get", "TEXT", "--profile", "app", "--env", "local"}); got["value"] != value {
					t.Fatal("override was not stored", got)
				}
				if got := outputData(t, app, []string{"var", "get", "TEXT", "--profile", "app"}); got["value"] != "common" {
					t.Fatal("override changed common value", got)
				}
				outputData(t, app, []string{"var", "unset", "TEXT", "--profile", "app", "--env", "local"})
				if got := outputData(t, app, []string{"var", "get", "TEXT", "--profile", "app", "--env", "local"}); got["value"] != "common" {
					t.Fatal("removing override did not restore common value", got)
				}
			})
		}
	}
}

func TestVariableInputSelectionMatchesSchema(t *testing.T) {
	app := testApp(t)
	file := filepath.Join(privateTempDir(t), "value")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	data := outputData(t, app, []string{"schema", "var", "set"})
	compiler := jsonschema.NewCompiler()
	const url = "urn:devtools:test:variable-input"
	if err := compiler.AddResource(url, data["input_schema"]); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	// Every presence combination, including explicit false, must agree between
	// runtime and schema. Empty --value remains a selected input.
	for _, guarded := range []bool{false, true} {
		for mask := 0; mask < 4; mask++ {
			for _, stdin := range []string{"", "false", "true"} {
				args := []string{"var", "set", "TEXT", "--profile", "app"}
				input := map[string]any{"args": []any{"TEXT"}, "profile": "app"}
				selected := 0
				if mask&1 != 0 {
					args = append(args, "--value", "")
					input["value"] = ""
					selected++
				}
				if mask&2 != 0 {
					args = append(args, "--file", file)
					input["file"] = file
					selected++
				}
				if stdin != "" {
					args = append(args, "--stdin="+stdin)
					input["stdin"] = stdin == "true"
					if stdin == "true" {
						selected++
					}
				}
				if guarded {
					revision, id := editToken(t, app), tasks.ID()
					args = append(args, "--revision", revision, "--request-id", id)
					input["revision"], input["request-id"] = revision, id
				}
				valid := selected == 1
				if err := schema.Validate(input); (err == nil) != valid {
					t.Fatalf("schema %v: %v", args, err)
				}
				reader := &observedSecretReader{}
				var out, diagnostic bytes.Buffer
				code := app.Run(context.Background(), args, IO{In: reader, Out: &out, Err: &diagnostic})
				if valid && code != 0 || !valid && (code != 2 || reader.read || out.Len() != 0) {
					t.Fatalf("%v: code=%d read=%t err=%s", args, code, reader.read, diagnostic.String())
				}
			}
		}
	}
}

func TestVariableInputFailuresPreserveStoredValue(t *testing.T) {
	app := testApp(t)
	outputData(t, app, []string{"var", "set", "TEXT", "--value", "before", "--profile", "app"})
	dir := privateTempDir(t)
	file := filepath.Join(dir, "value")
	for _, content := range []string{"bad\x00value", "bad\xffvalue"} {
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		for _, flags := range [][]string{{"--stdin"}, {"--file", file}} {
			args := append([]string{"var", "set", "TEXT", "--profile", "app"}, flags...)
			if code, _, diagnostic := invoke(t, app, content, args...); code != 2 || !strings.Contains(diagnostic, "invalid_argument") {
				t.Fatal(code, diagnostic)
			}
		}
	}
	for _, tc := range []struct {
		path string
		code int
	}{
		{dir, 2}, {filepath.Join(dir, "missing"), 1},
	} {
		code, out, diagnostic := invoke(t, app, "", "var", "set", "TEXT", "--profile", "app", "--file", tc.path)
		if code != tc.code || out != "" || strings.Contains(diagnostic, tc.path) {
			t.Fatal(code, diagnostic)
		}
		if tc.code == 1 && !strings.Contains(diagnostic, `"input_role":"variable"`) {
			t.Fatal("wrong input role", diagnostic)
		}
	}
	if got := outputData(t, app, []string{"var", "get", "TEXT", "--profile", "app"}); got["value"] != "before" {
		t.Fatal("failed input changed stored value")
	}
}

func TestVariableInputGuardsAndMissingEnvDoNotRead(t *testing.T) {
	app := testApp(t)
	for _, flags := range [][]string{
		{"--request-id", tasks.ID()}, {"--revision", strings.Repeat("a", 64)},
		{"--create-only"}, {"--env", "missing"},
	} {
		reader := &observedSecretReader{}
		var out, diagnostic bytes.Buffer
		args := append([]string{"var", "set", "TEXT", "--profile", "app", "--stdin"}, flags...)
		code := app.Run(context.Background(), args, IO{In: reader, Out: &out, Err: &diagnostic})
		if code == 0 || reader.read || out.Len() != 0 {
			t.Fatalf("%v: code=%d read=%t", args, code, reader.read)
		}
	}
}

func TestVariableInputReplayAcrossSourcesAndRemovedEnv(t *testing.T) {
	app := testApp(t)
	outputData(t, app, []string{"env", "create", "local", "--profile", "app"})
	const value = "  replay\r\n$NAME\n"
	file := filepath.Join(privateTempDir(t), "value")
	if err := os.WriteFile(file, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	base := []string{"var", "set", "TEXT", "--profile", "app", "--env", "local", "--revision", editToken(t, app), "--request-id", tasks.ID(), "--create-only"}
	first := outputData(t, app, append(append([]string{}, base...), "--value", value))
	for _, flags := range [][]string{{"--stdin"}, {"--file", file}} {
		code, out, diagnostic := invoke(t, app, value, append(append([]string{}, base...), flags...)...)
		var envelope struct{ Data map[string]any }
		if code != 0 || json.Unmarshal([]byte(out), &envelope) != nil || envelope.Data["replayed"] != true || envelope.Data["revision"] != first["revision"] {
			t.Fatal("same bytes did not replay", code, out, diagnostic)
		}
	}
	if code, _, diagnostic := invoke(t, app, "different", append(append([]string{}, base...), "--stdin")...); code != 3 || !strings.Contains(diagnostic, "request_conflict") {
		t.Fatal("different bytes replayed", code, diagnostic)
	}
	outputData(t, app, []string{"var", "unset", "TEXT", "--profile", "app", "--env", "local"})
	outputData(t, app, []string{"env", "remove", "local", "--profile", "app"})
	if got := outputData(t, app, append(append([]string{}, base...), "--file", file)); got["replayed"] != true {
		t.Fatal("removed env blocked replay")
	}
}

func TestVariableInputCancellationAndMissingStdin(t *testing.T) {
	app := testApp(t)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var out, diagnostic bytes.Buffer
	args := []string{"var", "set", "TEXT", "--profile", "app", "--stdin"}
	if code := app.Run(ctx, args, IO{In: reader, Out: &out, Err: &diagnostic}); code != 130 || out.Len() != 0 {
		t.Fatal("stdin cancellation failed", code, diagnostic.String())
	}
	out.Reset()
	diagnostic.Reset()
	if code := app.Run(context.Background(), args, IO{Out: &out, Err: &diagnostic}); code != 2 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "variable data") {
		t.Fatal("missing stdin accepted", code, diagnostic.String())
	}
}

func TestVariableInputRedirectAndDevice(t *testing.T) {
	app := testApp(t)
	path := filepath.Join(privateTempDir(t), "value")
	const value = "redirect\r\n"
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{path, os.DevNull} {
		file, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		var out, diagnostic bytes.Buffer
		code := app.Run(context.Background(), []string{"var", "set", "TEXT", "--profile", "app", "--stdin"}, IO{In: file, Out: &out, Err: &diagnostic})
		file.Close()
		want := 0
		if source == os.DevNull {
			want = 2
		}
		if code != want {
			t.Fatal(source, code, diagnostic.String())
		}
	}
	if got := outputData(t, app, []string{"var", "get", "TEXT", "--profile", "app"}); got["value"] != value {
		t.Fatal("redirected input changed")
	}
}

func TestVariableInputKeepsKindsAndVaultRequirements(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	if code, _, diagnostic := invoke(t, app, "synthetic-kind-test", "sec", "set", "TOKEN", "--profile", "app", "--stdin"); code != 0 {
		t.Fatal(code, diagnostic)
	}
	outputData(t, app, []string{"var", "set", "TEXT", "--profile", "app", "--value", "before"})
	file := filepath.Join(privateTempDir(t), "value")
	if err := os.WriteFile(file, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{"--stdin"}, {"--file", file}} {
		if code, _, diagnostic := invoke(t, app, "after", append([]string{"var", "set", "TOKEN", "--profile", "app"}, flags...)...); code != 3 || !strings.Contains(diagnostic, "kind_conflict") {
			t.Fatal("input method changed secret kind", code, diagnostic)
		}
	}
	outputData(t, app, []string{"vault", "lock"})
	for _, flags := range [][]string{{"--stdin"}, {"--file", file}} {
		if code, _, diagnostic := invoke(t, app, "after", append([]string{"var", "set", "TEXT", "--profile", "app"}, flags...)...); code != 3 || !strings.Contains(diagnostic, "vault_locked") {
			t.Fatal("input method bypassed vault lock", code, diagnostic)
		}
	}
	if got := outputData(t, app, []string{"var", "get", "TEXT", "--profile", "app"}); got["value"] != "before" {
		t.Fatal("locked write changed stored value")
	}
}
