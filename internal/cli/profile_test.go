package cli

import (
	"fmt"
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
	recipientBytes, err := os.ReadFile(recipient)
	if err != nil {
		t.Fatal(err)
	}
	recipientValue := strings.TrimSpace(string(recipientBytes))
	if code, _, diagnostic := invoke(t, source, "", "var", "set", "LEVEL", "--profile", "app", "--value", "portable"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, source, "PROFILE_TRANSFER_SECRET", "sec", "set", "TOKEN", "--profile", "app", "--stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, source, "", "task", "add", "--profile", "app", "--title", "Portable task", "--request-id", tasks.ID()); code != 0 {
		t.Fatal(diagnostic)
	}

	exported := outputData(t, source, []string{"profile", "export", "--output", archive, "--recipient", recipientValue})
	if changed, _ := exported["changed"].(bool); !changed {
		t.Fatalf("export did not report a change: %#v", exported)
	}
	exportedItem := exported["item"].(map[string]any)
	if exportedItem["profile"] != "app" || exportedItem["path"] != archive || exportedItem["values"] != true || exportedItem["tasks"] != true {
		t.Fatalf("unexpected export metadata: %#v", exportedItem)
	}

	previewArgs := []string{"profile", "import", "--file", archive, "--identity-file", identity}
	code, previewOut, diagnostic := invoke(t, destination, "", previewArgs...)
	if code != 0 || diagnostic != "" || strings.Contains(previewOut, "PROFILE_TRANSFER_SECRET") {
		t.Fatalf("import preview failed or leaked secret: code=%d out=%s err=%s", code, previewOut, diagnostic)
	}
	preview := outputData(t, destination, previewArgs)
	if preview["changed"] != false || preview["replayed"] != false || preview["target_exists"] != false || preview["safety_backup"] != "" {
		t.Fatalf("unexpected preview metadata: %#v", preview)
	}
	digest, ok := preview["digest"].(string)
	if !ok || len(digest) != 64 || preview["diff"].(map[string]any)["different"] != true {
		t.Fatalf("invalid import preview: %#v", preview)
	}
	if listed := outputData(t, destination, []string{"profile", "list"}); len(listed["items"].([]any)) != 0 {
		t.Fatalf("preview mutated destination: %#v", listed)
	}

	requestID := tasks.ID()
	args := append(append([]string{}, previewArgs...), "--apply", digest, "--request-id", requestID)
	imported := outputData(t, destination, args)
	if imported["changed"] != true || imported["replayed"] != false || imported["target_exists"] != false || imported["safety_backup"] != "" || imported["digest"] != digest {
		t.Fatalf("unexpected import metadata: %#v", imported)
	}
	importedItem := imported["item"].(map[string]any)
	if importedItem["profile"] != "app" || importedItem["source_profile"] != "app" || importedItem["values"] != true || importedItem["tasks"] != true {
		t.Fatalf("unexpected imported profile: %#v", importedItem)
	}

	replayed := outputData(t, destination, args)
	if replayed["changed"] != true || replayed["replayed"] != true || replayed["digest"] != digest {
		t.Fatalf("import retry was not replayed: %#v", replayed)
	}
	conflictArgs := append(append([]string{}, previewArgs...), "--as", "other", "--apply", digest, "--request-id", requestID)
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

	existingPreview := outputData(t, destination, previewArgs)
	if existingPreview["target_exists"] != true || existingPreview["changed"] != false {
		t.Fatalf("existing target preview mismatch: %#v", existingPreview)
	}
	existingDigest := existingPreview["digest"].(string)
	otherRequest := tasks.ID()
	code, out, diagnostic = invoke(t, destination, "", "profile", "import", "--file", archive, "--identity-file", identity, "--apply", existingDigest, "--request-id", otherRequest)
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"profile_exists"`) {
		t.Fatalf("existing profile should require --replace: code=%d out=%s err=%s", code, out, diagnostic)
	}
	if code, out, diagnostic = invoke(t, destination, "", "profile", "import", "--file", archive, "--identity-file", identity, "--apply", existingDigest); code != 2 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
		t.Fatalf("apply without request-id should fail: code=%d out=%s err=%s", code, out, diagnostic)
	}
	if code, out, diagnostic = invoke(t, destination, "", "profile", "import", "--file", archive, "--identity-file", identity, "--request-id", tasks.ID()); code != 2 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
		t.Fatalf("request-id without apply should fail: code=%d out=%s err=%s", code, out, diagnostic)
	}
}

func TestProfileListAndInspectHideStoredValues(t *testing.T) {
	app := testApp(t)
	profile := []string{"--profile", "app"}
	if code, _, diagnostic := invoke(t, app, "", append([]string{"env", "create", "local"}, profile...)...); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "", "var", "set", "PUBLIC", "--profile", "app", "--value", "PROFILE_INSPECT_VISIBLE"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "", "var", "set", "PUBLIC", "--profile", "app", "--env", "local", "--value", "PROFILE_INSPECT_OVERRIDE"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "PROFILE_INSPECT_SECRET", "sec", "set", "TOKEN", "--profile", "app", "--stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "", "task", "add", "--profile", "app", "--title", "Inspect task", "--request-id", tasks.ID()); code != 0 {
		t.Fatal(diagnostic)
	}

	listed := outputData(t, app, []string{"profile", "list"})
	items := listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("unexpected profile list: %#v", listed)
	}
	summary := items[0].(map[string]any)
	if summary["profile"] != "app" || summary["values"] != true || summary["tasks"] != true || summary["env_count"] != float64(1) {
		t.Fatalf("unexpected profile summary: %#v", summary)
	}

	code, out, diagnostic := invoke(t, app, "", "profile", "inspect", "app")
	if code != 0 || diagnostic != "" {
		t.Fatalf("inspect failed: code=%d out=%s err=%s", code, out, diagnostic)
	}
	for _, canary := range []string{"PROFILE_INSPECT_VISIBLE", "PROFILE_INSPECT_OVERRIDE", "PROFILE_INSPECT_SECRET"} {
		if strings.Contains(out, canary) {
			t.Fatalf("stored value leaked through profile inspect: %s", canary)
		}
	}
	data := outputData(t, app, []string{"profile", "inspect", "app"})
	item := data["item"].(map[string]any)
	if envs := item["envs"].([]any); len(envs) != 1 || envs[0] != "local" {
		t.Fatalf("unexpected envs: %#v", item["envs"])
	}
	variables := item["variables"].([]any)
	if len(variables) != 1 {
		t.Fatalf("unexpected variable metadata: %#v", variables)
	}
	variable := variables[0].(map[string]any)
	if variable["key"] != "PUBLIC" || variable["common"] != true || len(variable["envs"].([]any)) != 1 || variable["envs"].([]any)[0] != "local" {
		t.Fatalf("unexpected variable scope: %#v", variable)
	}
	secrets := item["secrets"].([]any)
	if len(secrets) != 1 || secrets[0].(map[string]any)["key"] != "TOKEN" {
		t.Fatalf("unexpected secret metadata: %#v", secrets)
	}
	counts := item["task_counts"].([]any)
	if len(counts) == 0 || counts[0].(map[string]any)["kind"] != "task" {
		t.Fatalf("unexpected task counts: %#v", counts)
	}
	if len(item["instances"].([]any)) != 0 || len(item["processes"].([]any)) != 0 {
		t.Fatalf("unexpected runtime metadata: %#v", item)
	}

	if code, out, diagnostic := invoke(t, app, "", "profile", "inspect", "missing"); code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"profile_not_found"`) {
		t.Fatalf("missing profile should fail explicitly: code=%d out=%s err=%s", code, out, diagnostic)
	}
}

func TestProfileInspectCountsOnlyCurrentTaskDefinitions(t *testing.T) {
	app := testApp(t)
	workstream := outputData(t, app, []string{"task", "workstream", "create", "--profile", "app", "--title", "Counts", "--request-id", tasks.ID()})
	workstreamID := workstream["item"].(map[string]any)["id"].(string)
	created := outputData(t, app, []string{"task", "add", "--profile", "app", "--workstream", workstreamID, "--title", "Removed", "--request-id", tasks.ID()})
	taskID := created["item"].(map[string]any)["id"].(string)
	body := fmt.Sprintf(`{"reason":"Remove from current definition","operations":[{"op":"task.remove","id":"%s"}]}`, taskID)
	if code, out, diagnostic := invoke(t, app, body, "task", "workstream", "edit", workstreamID, "--profile", "app", "--stdin", "--if-revision", fmt.Sprint(created["revision"]), "--request-id", tasks.ID()); code != 0 || diagnostic != "" {
		t.Fatalf("remove task: code=%d out=%s err=%s", code, out, diagnostic)
	}

	item := outputData(t, app, []string{"profile", "inspect", "app"})["item"].(map[string]any)
	counts := item["task_counts"].([]any)
	for _, raw := range counts {
		count := raw.(map[string]any)
		if count["kind"] == "task" {
			t.Fatalf("removed task leaked into current profile counts: %#v", counts)
		}
	}
}

func TestProfileDiffIsMetadataOnly(t *testing.T) {
	app := testApp(t)
	for _, profile := range []string{"left", "right"} {
		if code, _, diagnostic := invoke(t, app, "", "env", "create", "local", "--profile", profile); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	if code, _, diagnostic := invoke(t, app, "", "var", "set", "CONFIG", "--profile", "left", "--value", "LEFT_DIFF_VARIABLE"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "", "var", "set", "CONFIG", "--profile", "right", "--value", "RIGHT_DIFF_VARIABLE"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "", "var", "set", "CONFIG", "--profile", "right", "--env", "local", "--value", "RIGHT_DIFF_OVERRIDE"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "LEFT_DIFF_SECRET", "sec", "set", "TOKEN", "--profile", "left", "--stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "RIGHT_DIFF_SECRET", "sec", "set", "TOKEN", "--profile", "right", "--stdin"); code != 0 {
		t.Fatal(diagnostic)
	}

	code, out, diagnostic := invoke(t, app, "", "profile", "diff", "left", "right")
	if code != 0 || diagnostic != "" {
		t.Fatalf("diff failed: code=%d out=%s err=%s", code, out, diagnostic)
	}
	for _, canary := range []string{"LEFT_DIFF_VARIABLE", "RIGHT_DIFF_VARIABLE", "RIGHT_DIFF_OVERRIDE", "LEFT_DIFF_SECRET", "RIGHT_DIFF_SECRET"} {
		if strings.Contains(out, canary) {
			t.Fatalf("stored value leaked through CLI diff: %s", canary)
		}
	}
	data := outputData(t, app, []string{"profile", "diff", "left", "right"})
	if data["left"] != "left" || data["right"] != "right" || data["different"] != true {
		t.Fatalf("unexpected diff header: %#v", data)
	}
	variables := data["variables"].([]any)
	if len(variables) != 1 || variables[0].(map[string]any)["key"] != "CONFIG" || variables[0].(map[string]any)["action"] != "changed" {
		t.Fatalf("unexpected variable changes: %#v", variables)
	}
	if secrets := data["secrets"].([]any); len(secrets) != 0 {
		t.Fatalf("secret value equality must not be disclosed: %#v", secrets)
	}
	if _, mutation := data["changed"]; mutation {
		t.Fatalf("profile diff is a report, not a mutation: %#v", data)
	}
	if strings.Contains(out, `"processes"`) {
		t.Fatal("runtime processes leaked into profile diff")
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

	base := []string{"profile", "import", "--file", archive, "--identity-file", identity}
	if code, out, diagnostic := invoke(t, destination, "", base...); code != 2 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
		t.Fatalf("multi-profile import should require selection: code=%d out=%s err=%s", code, out, diagnostic)
	}

	previewArgs := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--profile", "two", "--as", "copied"}
	preview := outputData(t, destination, previewArgs)
	item := preview["item"].(map[string]any)
	if item["source_profile"] != "two" || item["profile"] != "copied" || preview["changed"] != false || preview["target_exists"] != false {
		t.Fatalf("explicit profile selection preview failed: %#v", preview)
	}
	digest := preview["digest"].(string)
	args := append(append([]string{}, previewArgs...), "--apply", digest, "--request-id", tasks.ID())
	imported := outputData(t, destination, args)
	if imported["changed"] != true || imported["replayed"] != false {
		t.Fatalf("explicit profile selection apply failed: %#v", imported)
	}
	variable := outputData(t, destination, []string{"var", "get", "VALUE", "--profile", "copied"})
	if variable["value"] != "second" {
		t.Fatalf("selected profile value was not imported: %#v", variable)
	}

	missing := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--profile", "missing"}
	if code, out, diagnostic := invoke(t, destination, "", missing...); code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"profile_not_found"`) {
		t.Fatalf("missing source profile should fail explicitly: code=%d out=%s err=%s", code, out, diagnostic)
	}
}
