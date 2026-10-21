package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

// The actual child validates the injected value without echoing it to logs.
func checkGeneratedSecretInput() int {
	token := os.Getenv("TOKEN")
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	defer clear(decoded)
	if err != nil || len(token) != 43 || len(decoded) != 32 {
		return 21
	}
	fmt.Fprint(os.Stdout, "generated-ok")
	return 0
}

func TestSecretGenerateCLIReplaysAndRunsWithoutValueOutput(t *testing.T) {
	app := testApp(t)
	args := []string{"secret", "generate", "TOKEN", "--profile", "app", "--revision", editToken(t, app), "--request-id", tasks.ID()}
	if code, _, diagnostic := invoke(t, app, "", args...); code != 3 || !strings.Contains(diagnostic, "key_missing") {
		t.Fatal("generation initialized protection", code)
	}
	initializeTestVault(t, app)
	args[6] = editToken(t, app)
	first := outputData(t, app, args)
	if len(first) != 4 || first["changed"] != true || first["replayed"] != false {
		t.Fatal("generation output changed or contains input")
	}
	if got := outputData(t, app, args); got["replayed"] != true || got["revision"] != first["revision"] {
		t.Fatal("generation retry did not replay")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if code, out, diagnostic := invoke(t, app, "", "run", "--profile", "app", "--", executable, "__generated-secret-check"); code != 0 || out != "generated-ok" || diagnostic != "" {
		t.Fatal("generated value not delivered intact to child", code)
	}
	args[6], args[8] = editToken(t, app), tasks.ID()
	if code, _, diagnostic := invoke(t, app, "", args...); code != 3 || !strings.Contains(diagnostic, "value_exists") {
		t.Fatal("generation default replaced secret", code)
	}
	args[0] = "sec"
	if got := outputData(t, app, append(append([]string{}, args...), "--replace")); got["changed"] != true {
		t.Fatal("explicit generated replacement failed")
	}
	frontend := outputData(t, app, []string{"secret", "generate", "VITE_TOKEN", "--profile", "app", "--revision", editToken(t, app), "--request-id", tasks.ID()})
	if warnings, ok := frontend["warnings"].([]any); !ok || len(warnings) != 1 {
		t.Fatal("generated frontend-prefixed secret lost warning")
	}
}

func TestSecretGenerateCLIRequiresGuardsAndNoInputOptions(t *testing.T) {
	app := testApp(t)
	for _, flags := range [][]string{
		{}, {"--revision", strings.Repeat("a", 64)}, {"--request-id", tasks.ID()},
		{"--revision", strings.Repeat("a", 64), "--request-id", tasks.ID(), "--stdin"},
		{"--revision", strings.Repeat("a", 64), "--request-id", tasks.ID(), "--length", "16"},
		{"--revision", strings.Repeat("a", 64), "--request-id", tasks.ID(), "--value", "synthetic-denied-input"},
	} {
		input := &observedSecretReader{}
		var out, diagnostic bytes.Buffer
		code := app.Run(context.Background(), append([]string{"secret", "generate", "TOKEN", "--profile", "app"}, flags...), IO{In: input, Out: &out, Err: &diagnostic})
		if code != 2 || input.read || out.Len() != 0 || strings.Contains(diagnostic.String(), "synthetic-denied-input") {
			t.Fatal("invalid generation input was consumed or disclosed", code)
		}
	}
	schema := outputData(t, app, []string{"schema", "sec", "generate"})
	input := schema["input_schema"].(map[string]any)
	properties := input["properties"].(map[string]any)
	for _, name := range []string{"file", "stdin", "value", "length", "encoding", "seed"} {
		if properties[name] != nil {
			t.Fatal("generation exposes unsupported recipe/input option")
		}
	}
}
