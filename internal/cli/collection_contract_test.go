package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectionCommandFiltersSortsAndContinuesOriginalSnapshot(t *testing.T) {
	a := testApp(t)
	dir, cache := privateTempDir(t), filepath.Join(privateTempDir(t), "query-cache")
	t.Chdir(dir)
	var config strings.Builder
	config.WriteString("profile='app-a'\n")
	for i := 0; i < 83; i++ {
		fmt.Fprintf(&config, "[commands.web%03d]\nexec=['echo','public']\n", i)
	}
	config.WriteString("[commands.worker]\nexec=['echo','public']\n")
	if e := os.WriteFile("devtools.toml", []byte(config.String()), 0600); e != nil {
		t.Fatal(e)
	}
	page := outputData(t, a, []string{"command", "list", "--query", "web", "--sort", "name", "--order", "desc", "--limit", "7", "--query-cache-dir", cache})
	seen := []string{}
	for {
		items := page["items"].([]any)
		if len(items) > 7 {
			t.Fatal(len(items))
		}
		for _, v := range items {
			seen = append(seen, v.(map[string]any)["name"].(string))
		}
		if page["next_cursor"] == nil {
			break
		}
		action := previewAction(t, page)
		if action.State != "ready" || a.ResolveAction(action.Argv) != nil {
			t.Fatal(action)
		}
		// A continuation must still return the original rows after config edits.
		if e := os.WriteFile("devtools.toml", []byte("profile='app-b'\n"), 0600); e != nil {
			t.Fatal(e)
		}
		page = outputData(t, a, action.Argv[1:])
	}
	if len(seen) != 83 {
		t.Fatal(len(seen))
	}
	for i, name := range seen {
		if name != fmt.Sprintf("web%03d", 82-i) {
			t.Fatal(i, name)
		}
	}
}

func TestCollectionColumnsAreTextOnlyAndRejectUnknownOrDuplicate(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	if e := os.WriteFile("devtools.toml", []byte("profile='app-a'\n[commands.web]\nexec=['echo','visible']\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{
		{"--format", "json", "command", "list", "--columns", "name"},
		{"--format", "text", "command", "list", "--columns", "missing"},
		{"--format", "text", "command", "list", "--columns", "name", "--columns", "name"},
		{"command", "list", "--columns", "name"}, // Auto on pipe resolves JSON.
	} {
		code, _, diagnostic := invoke(t, a, "", args...)
		if code != 2 || !strings.Contains(diagnostic, "columns") {
			t.Fatal(args, code, diagnostic)
		}
	}
	code, out, diagnostic := invoke(t, a, "", "--format", "text", "command", "list", "--columns", "name")
	if code != 0 || diagnostic != "" || !strings.Contains(out, "web") || strings.Contains(out, "visible") {
		t.Fatal(code, out, diagnostic)
	}
}

func TestCollectionScopeOutsideProjectRequiresExplicitSelection(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	code, _, diagnostic := invoke(t, a, "", "process", "list")
	if code != 3 || !strings.Contains(diagnostic, `"code":"scope_required"`) {
		t.Fatal(code, diagnostic)
	}
	for _, args := range [][]string{{"process", "list", "--all-profiles", "--profile", "app-a"}, {"command", "list", "--limit", "0"}, {"command", "list", "--limit", "201"}} {
		if code, _, e := invoke(t, a, "", args...); code != 2 {
			t.Fatal(args, code, e)
		}
	}
}
