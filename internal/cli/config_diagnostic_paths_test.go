package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigBodyErrorsKeepRelevantPathAndTypeFacts(t *testing.T) {
	root := privateTempDir(t)
	t.Chdir(root)
	file := filepath.Join(root, "devtools.toml")
	const original = "profile='fixture'\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	for _, tc := range []struct {
		args []string
		body string
		path []any
		text string
	}{
		{[]string{"command", "add", "other", "--stdin", "--dry-run"}, `{"exec":[42]}`, []any{"exec", "0"}, "exec[0]"},
		{[]string{"config", "edit", "--revision", strings.Repeat("0", 64), "--stdin", "--dry-run"}, `{"operations":[{"op":"command.add","name":"other","value":{"exec":[42]}}]}`, []any{"operations", "0", "value", "exec", "0"}, "operations[0].value.exec[0]"},
	} {
		code, _, diagnostic := invoke(t, app, tc.body, tc.args...)
		var env map[string]any
		if code != 2 || json.Unmarshal([]byte(diagnostic), &env) != nil {
			t.Fatal(code, diagnostic)
		}
		assertContract(t, "Failure", env)
		details := env["error"].(map[string]any)["details"].(map[string]any)
		if !reflect.DeepEqual(details["sources"].([]any)[0].(map[string]any)["path"], tc.path) {
			t.Fatal(details)
		}
		facts := details["facts"].(map[string]any)
		if facts["expected_type"] != "string" || facts["observed_type"] != "number" {
			t.Fatal(facts)
		}
		_, _, text := invoke(t, app, tc.body, append([]string{"--format", "text"}, tc.args...)...)
		if !strings.Contains(text, tc.text) || !strings.Contains(text, "Expected string; received number.") {
			t.Fatal(text)
		}
		if after, err := os.ReadFile(file); err != nil || string(after) != original {
			t.Fatal("invalid body changed config", err)
		}
	}
}
