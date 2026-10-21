package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

func editToken(t *testing.T, app *App) string {
	t.Helper()
	data := outputData(t, app, []string{"values", "revision", "--profile", "app"})
	if len(data) != 4 || data["integrity_verified"] != false {
		t.Fatal("revision query leaked values or changed contract")
	}
	return data["revision"].(string)
}

func TestGuardedInputsRejectBeforeReading(t *testing.T) {
	app := testApp(t)
	revision, id := strings.Repeat("a", 64), tasks.ID()
	for _, flags := range [][]string{
		{"--revision", revision}, {"--request-id", id}, {"--create-only"},
		{"--create-only=false"}, {"--revision", revision, "--request-id", id, "--file", "unused"},
	} {
		input := &observedSecretReader{}
		var out, diagnostic bytes.Buffer
		args := append([]string{"secret", "set", "TOKEN", "--profile", "app", "--stdin"}, flags...)
		code := app.Run(context.Background(), args, IO{In: input, Out: &out, Err: &diagnostic})
		if code != 2 || input.read || out.Len() != 0 {
			t.Fatal("invalid guards/input consumed stream", code, input.read)
		}
	}
	for _, args := range [][]string{
		{"variable", "set", "URL", "--value", "x"}, {"variable", "unset", "URL"},
		{"secret", "unset", "TOKEN"}, {"env", "create", "local"}, {"env", "remove", "local"},
		{"import", "--file", "unread-missing-file"},
	} {
		args = append(args, "--profile", "app", "--request-id", id)
		if code, _, diagnostic := invoke(t, app, "", args...); code != 2 || !strings.Contains(diagnostic, "invalid_argument") {
			t.Fatal("writer allowed half a guard", code)
		}
	}
}

func TestGuardedValuesAndEnvironmentReplay(t *testing.T) {
	app := testApp(t)
	// Legacy output stays exactly profile/changed, with no automation fields.
	legacy := outputData(t, app, []string{"var", "set", "URL", "--value", "before", "--profile", "app"})
	if !reflect.DeepEqual(legacy, map[string]any{"profile": "app", "changed": true}) {
		t.Fatal("legacy output changed")
	}
	revision, id := editToken(t, app), tasks.ID()
	args := []string{"var", "set", "URL", "--value", "after", "--profile", "app", "--revision", revision, "--request-id", id}
	first := outputData(t, app, args)
	if first["changed"] != true || first["replayed"] != false {
		t.Fatal("guarded fields missing")
	}
	outputData(t, app, []string{"var", "set", "URL", "--value", "latest", "--profile", "app"})
	if got := outputData(t, app, args); got["revision"] != first["revision"] || got["replayed"] != true {
		t.Fatal("lost-response retry did not preserve result")
	}
	if got := outputData(t, app, []string{"var", "get", "URL", "--profile", "app"}); got["value"] != "latest" {
		t.Fatal("replay changed later writer")
	}
	changed := append([]string{}, args...)
	changed[4] = "different"
	if code, _, diagnostic := invoke(t, app, "", changed...); code != 3 || !strings.Contains(diagnostic, "request_conflict") {
		t.Fatal("different input accepted", code)
	}
	changed = append([]string{}, args...)
	changed[len(changed)-1] = tasks.ID()
	if code, _, diagnostic := invoke(t, app, "", changed...); code != 3 || !strings.Contains(diagnostic, "revision_conflict") {
		t.Fatal("stale edit accepted", code)
	}
	request := outputData(t, app, []string{"values", "request", "show", id, "--profile", "app"})
	item := request["item"].(map[string]any)
	if item["state"] != "completed" || item["result"].(map[string]any)["replayed"] != false {
		t.Fatal("lookup returned live state instead of original outcome")
	}
	create := []string{"env", "create", "local", "--profile", "app", "--revision", editToken(t, app), "--request-id", tasks.ID()}
	outputData(t, app, create)
	remove := []string{"env", "remove", "local", "--profile", "app", "--revision", editToken(t, app), "--request-id", tasks.ID()}
	removed := outputData(t, app, remove)
	if got := outputData(t, app, create); got["replayed"] != true {
		t.Fatal("removed environment blocked create replay")
	}
	if got := outputData(t, app, remove); got["replayed"] != true || got["revision"] != removed["revision"] {
		t.Fatal("removed environment blocked remove replay")
	}
}

func TestGuardedSecretByteIdentityAndRemovedEnvironment(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	outputData(t, app, []string{"env", "create", "local", "--profile", "app"})
	const canary = "synthetic-guarded-secret\r\nsecond\n"
	file := filepath.Join(privateTempDir(t), "private-input")
	if err := os.WriteFile(file, []byte(canary), 0600); err != nil {
		t.Fatal(err)
	}
	id := tasks.ID()
	base := []string{"secret", "set", "TOKEN", "--profile", "app", "--env", "local", "--create-only", "--revision", editToken(t, app), "--request-id", id}
	call := func(input string, suffix ...string) map[string]any {
		t.Helper()
		code, out, diagnostic := invoke(t, app, input, append(append([]string{}, base...), suffix...)...)
		if code != 0 || diagnostic != "" || strings.Contains(out, canary) {
			t.Fatal("private guarded request failed or leaked", code)
		}
		var envelope struct{ Data map[string]any }
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	first := call("", "--file", file)
	if call(canary, "--stdin")["replayed"] != true {
		t.Fatal("equivalent file/stdin bytes did not replay")
	}
	if err := os.WriteFile(file, []byte("synthetic-different"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := invoke(t, app, "", append(append([]string{}, base...), "--file", file)...); code != 3 || !strings.Contains(diagnostic, "request_conflict") {
		t.Fatal("changed file replayed", code)
	}
	outputData(t, app, []string{"secret", "unset", "TOKEN", "--profile", "app", "--env", "local", "--revision", editToken(t, app), "--request-id", tasks.ID()})
	outputData(t, app, []string{"env", "remove", "local", "--profile", "app"})
	if got := call(canary, "--stdin"); got["replayed"] != true || got["revision"] != first["revision"] {
		t.Fatal("removed environment blocked committed secret replay")
	}
}

func TestGuardedImportPreservesLayerPreviewAndRequestIdentity(t *testing.T) {
	for _, bundle := range []string{"", "backend"} {
		t.Run("bundle="+bundle, func(t *testing.T) {
			app := testApp(t)
			outputData(t, app, []string{"var", "set", "URL", "--value", "common", "--profile", "app"})
			outputData(t, app, []string{"env", "create", "local", "--profile", "app"})
			file := filepath.Join(privateTempDir(t), "public.env")
			if err := os.WriteFile(file, []byte("URL=override\n"), 0600); err != nil {
				t.Fatal(err)
			}
			id := tasks.ID()
			args := []string{"import", "--file", file, "--profile", "app", "--env", "local", "--var", "URL", "--revision", editToken(t, app), "--request-id", id}
			if bundle != "" {
				args = append(args, "--bundle", bundle)
			}
			preview := outputData(t, app, append(append([]string{}, args...), "--dry-run"))
			if preview["changed"] != false || preview["applicable"] != true {
				t.Fatal("preview changed or suppressed layer add")
			}
			if code, _, diagnostic := invoke(t, app, "", "values", "request", "show", id, "--profile", "app"); code != 3 || !strings.Contains(diagnostic, "request_not_found") {
				t.Fatal("preview consumed UUID", code)
			}
			first := outputData(t, app, args)
			if first["changed"] != true || first["replayed"] != false || first["items"].([]any)[0].(map[string]any)["action"] != "add" {
				t.Fatal("guarded import lost CLI layer semantics")
			}
			outputData(t, app, []string{"var", "unset", "URL", "--profile", "app", "--env", "local"})
			outputData(t, app, []string{"env", "remove", "local", "--profile", "app"})
			if got := outputData(t, app, args); got["replayed"] != true || got["revision"] != first["revision"] {
				t.Fatal("committed import blocked by current env or bundle")
			}
		})
	}
}

func TestGuardedSchemaComposesSecretInputAndPair(t *testing.T) {
	app := testApp(t)
	for _, name := range []string{"secret set", "variable set", "secret unset", "env create", "env remove", "import"} {
		data := outputData(t, app, append([]string{"schema"}, strings.Fields(name)...))
		input := data["input_schema"].(map[string]any)
		if len(input["allOf"].([]any)) != 1 {
			t.Fatal("missing guard constraint", name)
		}
		if name == "secret set" && len(input["oneOf"].([]any)) != 2 {
			t.Fatal("guard constraint replaced file/stdin exclusivity")
		}
		properties := data["output_schema"].(map[string]any)["properties"].(map[string]any)
		if properties["revision"] == nil || properties["replayed"] == nil {
			t.Fatal("missing opt-in output fields", name)
		}
	}
}

func TestGuardedImportLimitsKeepLegacyPreview(t *testing.T) {
	app := testApp(t)
	file := filepath.Join(privateTempDir(t), "bounded.env")
	var content strings.Builder
	for n := range 5001 {
		fmt.Fprintf(&content, "KEY%d=synthetic-value\n", n)
	}
	if err := os.WriteFile(file, []byte(content.String()), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"import", "--file", file, "--profile", "app", "--dry-run"}
	if code, _, _ := invoke(t, app, "", args...); code != 0 {
		t.Fatal("legacy preview acquired guarded key limit", code)
	}
	guarded := append(append([]string{}, args...), "--revision", editToken(t, app), "--request-id", tasks.ID())
	if code, _, diagnostic := invoke(t, app, "", guarded...); code != 2 || !strings.Contains(diagnostic, "5,000") {
		t.Fatal("guarded key limit missing", code)
	}
	if err := os.WriteFile(file, []byte("KEY="+strings.Repeat("x", 500001)), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := invoke(t, app, "", guarded...); code != 2 || !strings.Contains(diagnostic, "invalid_argument") {
		t.Fatal("guarded byte limit missing", code)
	}
}
