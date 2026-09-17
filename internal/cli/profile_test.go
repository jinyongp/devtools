package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProfileExportImportAcrossStores(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Chdir(root)
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}

	source := testApp(t)
	destination := testApp(t)
	identity := filepath.Join(root, "identity.txt")
	recipient := filepath.Join(root, "recipient.txt")
	archive := filepath.Join(root, "app.age")
	if code, _, diagnostic := invoke(t, source, "", "backup", "keygen", "--identity-file", identity, "--recipient-file", recipient); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, source, "", "var", "set", "LEVEL", "--profile", "app", "--value", "portable"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, source, "PROFILE_TRANSFER_SECRET", "sec", "set", "TOKEN", "--profile", "app", "--stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, source, "", "task", "add", "--profile", "app", "--title", "Portable task", "--request-id", tasks.ID()); code != 0 {
		t.Fatal(diagnostic)
	}

	exported := outputData(t, source, []string{"profile", "export", "--output", archive, "--recipient-file", recipient})
	if changed, _ := exported["changed"].(bool); !changed {
		t.Fatalf("export did not report a change: %#v", exported)
	}
	exportedItem := exported["item"].(map[string]any)
	if exportedItem["profile"] != "app" || exportedItem["path"] != archive || exportedItem["values"] != true || exportedItem["tasks"] != true {
		t.Fatalf("unexpected export metadata: %#v", exportedItem)
	}

	requestID := tasks.ID()
	args := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--request-id", requestID}
	imported := outputData(t, destination, args)
	if imported["changed"] != true || imported["replayed"] != false || imported["safety_backup"] != "" {
		t.Fatalf("unexpected import metadata: %#v", imported)
	}
	importedItem := imported["item"].(map[string]any)
	if importedItem["profile"] != "app" || importedItem["source_profile"] != "app" || importedItem["values"] != true || importedItem["tasks"] != true {
		t.Fatalf("unexpected imported profile: %#v", importedItem)
	}

	replayed := outputData(t, destination, args)
	if replayed["changed"] != true || replayed["replayed"] != true {
		t.Fatalf("import retry was not replayed: %#v", replayed)
	}
	conflictArgs := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--as", "other", "--request-id", requestID}
	if code, out, diagnostic := invoke(t, destination, "", conflictArgs...); code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"request_conflict"`) {
		t.Fatalf("changed retry input should conflict: code=%d out=%s err=%s", code, out, diagnostic)
	}

	variable := outputData(t, destination, []string{"var", "get", "LEVEL", "--profile", "app"})
	if variable["value"] != "portable" {
		t.Fatalf("variable not imported: %#v", variable)
	}
	code, out, diagnostic := invoke(t, destination, "", "command", "run", "--profile", "app", "--", "/bin/sh", "-c", `printf %s "$TOKEN"`)
	if code != 0 || diagnostic != "" || out != "PROFILE_TRANSFER_SECRET" {
		t.Fatalf("secret not imported: code=%d out=%q err=%q", code, out, diagnostic)
	}
	taskList := outputData(t, destination, []string{"task", "list", "--profile", "app", "--state", "all"})
	items := taskList["items"].([]any)
	if len(items) != 1 || !strings.Contains(items[0].(map[string]any)["title"].(string), "Portable task") {
		t.Fatalf("task state not imported: %#v", taskList)
	}

	otherRequest := tasks.ID()
	code, out, diagnostic = invoke(t, destination, "", "profile", "import", "--file", archive, "--identity-file", identity, "--request-id", otherRequest)
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"profile_exists"`) {
		t.Fatalf("existing profile should require --replace: code=%d out=%s err=%s", code, out, diagnostic)
	}
}

func TestProfileImportRequiresSelectionForMultiProfileArchive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Chdir(root)

	source := testApp(t)
	destination := testApp(t)
	identity := filepath.Join(root, "identity.txt")
	recipient := filepath.Join(root, "recipient.txt")
	archive := filepath.Join(root, "multiple.age")
	if code, _, diagnostic := invoke(t, source, "", "backup", "keygen", "--identity-file", identity, "--recipient-file", recipient); code != 0 {
		t.Fatal(diagnostic)
	}
	for profile, value := range map[string]string{"one": "first", "two": "second"} {
		if code, _, diagnostic := invoke(t, source, "", "var", "set", "VALUE", "--profile", profile, "--value", value); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	if data := outputData(t, source, []string{"backup", "create", "--output", archive, "--recipient-file", recipient}); data["changed"] != true {
		t.Fatalf("multi-profile backup was not created: %#v", data)
	}

	base := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--request-id", tasks.ID()}
	if code, out, diagnostic := invoke(t, destination, "", base...); code != 2 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
		t.Fatalf("multi-profile import should require selection: code=%d out=%s err=%s", code, out, diagnostic)
	}

	args := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--profile", "two", "--as", "copied", "--request-id", tasks.ID()}
	imported := outputData(t, destination, args)
	item := imported["item"].(map[string]any)
	if item["source_profile"] != "two" || item["profile"] != "copied" {
		t.Fatalf("explicit profile selection failed: %#v", imported)
	}
	variable := outputData(t, destination, []string{"var", "get", "VALUE", "--profile", "copied"})
	if variable["value"] != "second" {
		t.Fatalf("selected profile value was not imported: %#v", variable)
	}

	missing := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--profile", "missing", "--request-id", tasks.ID()}
	if code, out, diagnostic := invoke(t, destination, "", missing...); code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"profile_not_found"`) {
		t.Fatalf("missing source profile should fail explicitly: code=%d out=%s err=%s", code, out, diagnostic)
	}
}
