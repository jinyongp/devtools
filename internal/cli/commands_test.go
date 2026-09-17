package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCommandListInspectAndRunAlias(t *testing.T) {
	app := testApp(t)
	root := t.TempDir()
	config := `profile = "app"
[requirements]
vars = ["ROOT_VALUE"]

[commands.zeta]
exec = ["/bin/sh", "-c", "printf zeta"]
inject = true

[commands.dev]
exec = ["/bin/sh", "-c", "printf dev"]
inject = true
env = "local"
[commands.dev.requirements]
secs = ["TOKEN"]
[commands.dev.requirements.tools.go]
version = "1.25.0"
[commands.dev.ready]
exec = ["/bin/true"]
`
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	code, out, diagnostic := invoke(t, app, "", "command", "list", "--dir", root)
	if code != 0 || diagnostic != "" {
		t.Fatalf("list: %d %s %s", code, out, diagnostic)
	}
	var listed struct {
		Data struct {
			Profile  string                  `json:"profile"`
			Commands []projectCommandSummary `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Data.Profile != "app" || len(listed.Data.Commands) != 2 || listed.Data.Commands[0].Name != "dev" || listed.Data.Commands[1].Name != "zeta" {
		t.Fatalf("unexpected list: %+v", listed.Data)
	}
	dev := listed.Data.Commands[0]
	if strings.Join(dev.Exec, " ") != "/bin/sh -c printf dev" || !dev.Inject || dev.Env != "local" || len(dev.Serve) != 0 {
		t.Fatalf("unexpected command summary: %+v", dev)
	}

	code, out, diagnostic = invoke(t, app, "", "command", "inspect", "dev", "--dir", root)
	if code != 0 || diagnostic != "" {
		t.Fatalf("inspect: %d %s %s", code, out, diagnostic)
	}
	var inspected struct {
		Data struct {
			Profile string                `json:"profile"`
			Command projectCommandDetails `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &inspected); err != nil {
		t.Fatal(err)
	}
	details := inspected.Data.Command
	if inspected.Data.Profile != "app" || details.Name != "dev" || details.Ready == nil || details.Ready.Timeout != "2s" {
		t.Fatalf("unexpected command details: %+v", inspected.Data)
	}
	if strings.Join(details.Requirements.Vars, ",") != "ROOT_VALUE" || strings.Join(details.Requirements.Secs, ",") != "TOKEN" {
		t.Fatalf("requirements not merged: %+v", details.Requirements)
	}
	if tool, ok := details.Requirements.Tools["go"]; !ok || tool.Executable != "go" || tool.Version != "1.25.0" || strings.Join(tool.VersionArgs, " ") != "--version" {
		t.Fatalf("tool requirement defaults not resolved: %+v", details.Requirements.Tools)
	}
	if strings.Contains(out, `"version_args":null`) {
		t.Fatalf("command inspect violated its array schema: %s", out)
	}

	code, out, diagnostic = invoke(t, app, "", "command", "inspect", "missing", "--dir", root)
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"command_not_found"`) {
		t.Fatalf("missing: %d %q %q", code, out, diagnostic)
	}

	t.Chdir(root)
	if code, _, diagnostic = invoke(t, app, "", "var", "set", "ROOT_VALUE", "--value", "ready"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, args := range [][]string{{"command", "run", "zeta"}, {"run", "zeta"}} {
		code, out, diagnostic = invoke(t, app, "", args...)
		if code != 0 || out != "zeta" || diagnostic != "" {
			t.Fatalf("%v: %d %q %q", args, code, out, diagnostic)
		}
	}
}
