package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportCLI(t *testing.T) {
	a := testApp(t)
	file := filepath.Join(t.TempDir(), ".env")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	call := func(want int, args ...string) string {
		t.Helper()
		code, out, diagnostic := invoke(t, a, "", append([]string{"import", "--profile", "app", "--file", file}, args...)...)
		if code != want || strings.Contains(out+diagnostic, "CANARY") {
			t.Fatalf("exit %d out %s diagnostic %s", code, out, diagnostic)
		}
		return out + diagnostic
	}
	write("PORT=3000\nMODE=dev\nTOKEN=CANARY\n")
	out := call(0, "--var", "PORT", "--var", "MODE", "--dry-run")
	if !strings.Contains(out, `"changed":false`) || !strings.Contains(out, `"kind":"secret"`) {
		t.Fatal(out)
	}
	dir, _ := a.dataDirectory()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("dry-run created storage")
	}
	call(2, "--var", "BAD-KEY", "--var", "PORT")
	call(2, "--var", "TYPO")
	call(3, "--env", "missing")
	call(0, "--var", "PORT", "--var", "MODE")
	if out = call(0); !strings.Contains(out, `"changed":false`) {
		t.Fatal(out)
	}
	call(3, "--var", "TOKEN", "--overwrite")
	write("PORT=4000\nTOKEN=CANARY-new\nNEW=CANARY-new\n")
	out = call(0, "--dry-run")
	if !strings.Contains(out, `"applicable":false`) || !strings.Contains(out, `"action":"conflict"`) {
		t.Fatal(out)
	}
	call(3)
	if code, _, _ := invoke(t, a, "", "var", "get", "NEW", "--profile", "app"); code != 3 {
		t.Fatal("partial import")
	}
	call(0, "--overwrite")
	if code, out, diagnostic := invoke(t, a, "", "var", "get", "PORT", "--profile", "app"); code != 0 || !strings.Contains(out, `"value":"4000"`) {
		t.Fatalf("%s %s", out, diagnostic)
	}
	write("PORT=5000\nBROKEN=\"CANARY\n")
	call(2)
}

func TestImportSchema(t *testing.T) {
	a := testApp(t)
	_, out, _ := invoke(t, a, "", "schema")
	var envelope struct {
		Data struct {
			Commands []struct {
				Name  string
				Input map[string]any `json:"input_schema"`
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, c := range envelope.Data.Commands {
		if c.Name == "import" {
			property := c.Input["properties"].(map[string]any)["var"].(map[string]any)
			if property["type"] != "array" || property["items"].(map[string]any)["pattern"] == nil {
				t.Fatal(property)
			}
			return
		}
	}
	t.Fatal("missing import schema")
}
