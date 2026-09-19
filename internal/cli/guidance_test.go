package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuidanceResolveUsesActualTargetAndProjectBoundary(t *testing.T) {
	app := New("test", "test")
	root := t.TempDir()
	nested := filepath.Join(root, "packages", "api")
	if err := os.MkdirAll(filepath.Join(nested, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='guidance-test'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("api rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code, out, diagnostic := invoke(t, app, "", "guidance", "resolve", "src/new.go", "--dir", nested)
	if code != 0 || diagnostic != "" {
		t.Fatalf("resolve: %d %q %q", code, out, diagnostic)
	}
	data := decodeCLIData(t, out)
	if data["target"] != "packages/api/src/new.go" || data["target_dir"] != "packages/api/src" || data["complete"] != true {
		t.Fatalf("data = %#v", data)
	}
	sources := data["sources"].([]any)
	if len(sources) != 2 ||
		sources[0].(map[string]any)["path"] != "AGENTS.md" ||
		sources[1].(map[string]any)["path"] != "packages/api/AGENTS.md" {
		t.Fatalf("sources = %#v", sources)
	}
	if !strings.Contains(sources[0].(map[string]any)["content"].(string), "root rules") ||
		!strings.Contains(sources[1].(map[string]any)["content"].(string), "api rules") {
		t.Fatalf("contents = %#v", sources)
	}

	code, _, diagnostic = invoke(t, app, "", "guidance", "resolve", filepath.Join(t.TempDir(), "outside.go"), "--dir", nested)
	if code != 3 || !strings.Contains(diagnostic, "guidance_boundary") {
		t.Fatalf("outside: %d %q", code, diagnostic)
	}
}

func TestGuidanceResolveSchemaAndHelp(t *testing.T) {
	app := New("test", "test")
	for _, args := range [][]string{{"guidance", "--help"}, {"guidance", "resolve", "--help"}} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 0 || diagnostic != "" || !strings.Contains(out, "AGENTS.md") {
			t.Fatalf("%v: %d %q %q", args, code, out, diagnostic)
		}
	}
	code, out, diagnostic := invoke(t, app, "", "schema", "--all")
	if code != 0 || diagnostic != "" {
		t.Fatalf("schema: %d %q", code, diagnostic)
	}
	if !strings.Contains(out, "\"name\":\"guidance resolve\"") ||
		!strings.Contains(out, "\"complete\"") ||
		!strings.Contains(out, "\"sources\"") {
		t.Fatalf("guidance schema missing: %s", out)
	}
}
