package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCLISkill(t *testing.T, parent, name, description string) string {
	t.Helper()
	root := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(root, "references"), 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "references", "guide.md"), []byte("guide"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func decodeCLIData(t *testing.T, output string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", envelope["data"])
	}
	return data
}

func TestSkillCommandsDiscoverInspectAndRegister(t *testing.T) {
	app := New("test", "test")
	project := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourceParent := t.TempDir()
	projectSource := writeCLISkill(t, sourceParent, "review-skill", "Use for project reviews.")

	code, out, diagnostic := invoke(t, app, "", "skill", "register", projectSource, "--scope", "project", "--dir", project)
	if code != 0 || diagnostic != "" {
		t.Fatalf("register: %d %q %q", code, out, diagnostic)
	}
	registered := decodeCLIData(t, out)
	if registered["changed"] != true {
		t.Fatalf("register result = %#v", registered)
	}

	userSourceParent := t.TempDir()
	userSource := writeCLISkill(t, userSourceParent, "user-skill", "Use from user scope.")
	code, out, diagnostic = invoke(t, app, "", "skill", "register", userSource, "--scope", "user", "--dir", project)
	if code != 0 || diagnostic != "" {
		t.Fatalf("user register: %d %q %q", code, out, diagnostic)
	}

	code, out, diagnostic = invoke(t, app, "", "skill", "list", "--dir", project)
	if code != 0 || diagnostic != "" {
		t.Fatalf("list: %d %q %q", code, out, diagnostic)
	}
	listed := decodeCLIData(t, out)
	items := listed["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["name"] != "review-skill" || items[1].(map[string]any)["name"] != "user-skill" {
		t.Fatalf("items = %#v", items)
	}
	if _, exists := items[0].(map[string]any)["content"]; exists {
		t.Fatal("skill list returned full content")
	}

	code, out, diagnostic = invoke(t, app, "", "skill", "inspect", "review-skill", "--dir", project)
	if code != 0 || diagnostic != "" {
		t.Fatalf("inspect: %d %q %q", code, out, diagnostic)
	}
	inspected := decodeCLIData(t, out)["item"].(map[string]any)
	if inspected["scope"] != "project" || !strings.Contains(inspected["content"].(string), "# review-skill") || len(inspected["resources"].([]any)) != 1 {
		t.Fatalf("inspect item = %#v", inspected)
	}

	code, _, diagnostic = invoke(t, app, "", "skill", "register", projectSource, "--scope", "project", "--dir", project)
	if code != 3 || !strings.Contains(diagnostic, "skill_exists") {
		t.Fatalf("collision: %d %q", code, diagnostic)
	}
}

func TestSkillProjectScopeOverridesUserScope(t *testing.T) {
	app := New("test", "test")
	project := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectSkills := filepath.Join(project, ".agents", "skills")
	userSkills := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(projectSkills, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(userSkills, 0755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, userSkills, "shared-skill", "User version.")
	writeCLISkill(t, projectSkills, "shared-skill", "Project version.")

	code, out, diagnostic := invoke(t, app, "", "skill", "list", "--dir", project)
	if code != 0 || diagnostic != "" {
		t.Fatalf("list: %d %q", code, diagnostic)
	}
	data := decodeCLIData(t, out)
	items := data["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["scope"] != "project" || items[0].(map[string]any)["description"] != "Project version." {
		t.Fatalf("effective items = %#v", items)
	}
	shadowed := data["shadowed"].([]any)
	if len(shadowed) != 1 || shadowed[0].(map[string]any)["shadowed_scope"] != "user" {
		t.Fatalf("shadowed = %#v", shadowed)
	}
}

func TestSkillCommandSchemasAndHelp(t *testing.T) {
	app := New("test", "test")
	for _, args := range [][]string{{"skill", "--help"}, {"skill", "list", "--help"}, {"skill", "inspect", "--help"}, {"skill", "register", "--help"}} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 0 || diagnostic != "" || !strings.Contains(out, "Agent Skill") {
			t.Fatalf("%v: %d %q %q", args, code, out, diagnostic)
		}
	}
	code, out, diagnostic := invoke(t, app, "", "schema", "--all")
	if code != 0 || diagnostic != "" {
		t.Fatalf("schema: %d %q", code, diagnostic)
	}
	var envelope struct {
		Data struct {
			Commands []map[string]any `json:"commands"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, command := range envelope.Data.Commands {
		name, _ := command["name"].(string)
		if strings.HasPrefix(name, "skill ") {
			found[name] = true
		}
	}
	for _, name := range []string{"skill list", "skill inspect", "skill register"} {
		if !found[name] {
			t.Errorf("missing %s", name)
		}
	}
}
