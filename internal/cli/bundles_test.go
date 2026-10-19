package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundleCLIImportSelectionAndAtomicEdit(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	if code, _, err := invoke(t, app, "", "init", "--profile", "app"); code != 0 {
		t.Fatal(err)
	}
	file := filepath.Join(root, "public.env")
	if err := os.WriteFile(file, []byte("URL=one\nOTHER=two\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data := outputData(t, app, []string{"import", "--file", file, "--var", "URL", "--var", "OTHER", "--bundle", "frontend"})
	if data["changed"] != true || data["bundle"] != "frontend" {
		t.Fatal("bundle import unchanged")
	}
	list := outputData(t, app, []string{"bundle", "list"})
	if len(list["items"].([]any)) != 1 {
		t.Fatal("bundle not created")
	}
	args := []string{"bundle", "set", "frontend", "--key", "URL", "--revision", list["revision"].(string), "--request-id", "00000000-0000-4000-8000-000000000997"}
	edited := outputData(t, app, args)
	if edited["changed"] != true {
		t.Fatal("membership edit unchanged")
	}
	replay := outputData(t, app, args)
	if replay["replayed"] != true {
		t.Fatal("same UUID did not replay")
	}
	if code, _, err := invoke(t, app, "", "import", "--file", file, "--var", "URL", "--var", "OTHER", "--bundle", "none"); code == 0 || err == "" {
		t.Fatal("reserved bundle name accepted")
	}
	// Adding bundle selection must not change the CLI's existing conflict rule.
	if err := os.WriteFile(file, []byte("URL=changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, err := invoke(t, app, "", "import", "--file", file, "--var", "URL", "--bundle", "frontend"); code != 3 || err == "" {
		t.Fatal("bundle import suppressed overwrite conflict")
	}
	listed := outputData(t, app, []string{"bundle", "list"})
	if listed["revision"] != edited["revision"] {
		t.Fatal("failed import partially published")
	}
}
