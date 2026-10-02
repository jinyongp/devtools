package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
)

func TestProjectCommandListInspectAndRunAlias(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
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
	if aliasCode, aliasOut, aliasDiagnostic := invoke(t, app, "", "cmd", "list", "--dir", root); aliasCode != 0 || aliasOut != out || aliasDiagnostic != "" {
		t.Fatalf("cmd list: %d %s %s", aliasCode, aliasOut, aliasDiagnostic)
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
	if aliasCode, aliasOut, aliasDiagnostic := invoke(t, app, "", "cmd", "inspect", "dev", "--dir", root); aliasCode != 0 || aliasOut != out || aliasDiagnostic != "" {
		t.Fatalf("cmd inspect: %d %s %s", aliasCode, aliasOut, aliasDiagnostic)
	}

	code, out, diagnostic = invoke(t, app, "", "command", "inspect", "missing", "--dir", root)
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"command_not_found"`) {
		t.Fatalf("missing: %d %q %q", code, out, diagnostic)
	}

	t.Chdir(root)
	if code, _, diagnostic = invoke(t, app, "", "var", "set", "ROOT_VALUE", "--value", "ready"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, args := range [][]string{{"command", "run", "zeta"}, {"cmd", "run", "zeta"}, {"run", "zeta"}} {
		code, out, diagnostic = invoke(t, app, "", args...)
		if code != 0 || out != "zeta" || diagnostic != "" {
			t.Fatalf("%v: %d %q %q", args, code, out, diagnostic)
		}
	}
}

func TestLiteralProjectCommandNames(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	names := []string{"docs:dev", "@docs/dev", "docs=dev", "docs dev", "문서:개발", "docs'\"$;`dev", "/^docs:/"}
	var config strings.Builder
	config.WriteString("profile='app'\n")
	for _, name := range names {
		key, _ := json.Marshal(name)
		fmt.Fprintf(&config, "[commands.%s]\nexec=['printf','%%s','ran']\n", key)
	}
	if err := os.WriteFile("devtools.toml", []byte(config.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for _, prefix := range [][]string{{"run"}, {"command", "run"}, {"cmd", "run"}} {
			args := append(append([]string{}, prefix...), name)
			code, out, stderr := invoke(t, app, "", args...)
			if code != 0 || out != "ran" || stderr != "" {
				t.Fatalf("%q: %d %q %q", args, code, out, stderr)
			}
		}
		code, out, stderr := invoke(t, app, "", "command", "inspect", name)
		var response struct {
			Data struct {
				Item projectCommandDetails `json:"item"`
			} `json:"data"`
		}
		if code != 0 || stderr != "" || json.Unmarshal([]byte(out), &response) != nil || response.Data.Item.Name != name {
			t.Fatalf("inspect lost key %q: %d %q %q", name, code, out, stderr)
		}
		for _, prefix := range [][]string{{"run"}, {"command", "inspect"}, {"process", "start"}, {"project", "up"}, {"project", "restart"}, {"project", "logs"}} {
			words := append(append([]string{}, prefix...), name)
			code, out, stderr := invoke(t, app, strings.Join(words, "\x00")+"\x00", "__complete")
			if code != 0 || out != name+"\n" || stderr != "" {
				t.Fatalf("completion lost key %q: %d %q %q", words, code, out, stderr)
			}
		}
	}
	code, _, stderr := invoke(t, app, "", "run", "docs:dev", "--profile", "app:bad")
	if code != 2 || !strings.Contains(stderr, `"field":"profile"`) {
		t.Fatalf("profile identifier rules changed: %d %q", code, stderr)
	}
}

func TestAllCommandNameArguments(t *testing.T) {
	app := New("test", "test")
	for _, command := range app.commands {
		switch command.Name {
		case "command run", "command inspect", "process start", "project up", "project status", "project restart", "project logs", "project down", "doctor":
		default:
			continue
		}
		for _, argument := range command.Arguments {
			if argument.Name != "command" {
				continue
			}
			if argument.Pattern != project.CommandPattern {
				t.Fatalf("%s uses a different command name contract: %q", command.Name, argument.Pattern)
			}
			for _, name := range []string{"docs:dev", "@docs/dev", "문서 개발"} {
				args := []string{name}
				for _, option := range command.Options {
					if option.Name == "request-id" {
						args = append(args, "--request-id", "00000000-0000-0000-0000-000000000001")
					}
				}
				if _, err := parseRequest(command, args); err != nil {
					t.Fatalf("%s rejected %q: %v", command.Name, name, err)
				}
			}
			for _, name := range []string{"bad\nname", "bad\tname", strings.Repeat("x", 129)} {
				args := []string{name, "--request-id", "00000000-0000-0000-0000-000000000001"}
				if command.Name == "command run" || command.Name == "command inspect" || command.Name == "project status" || command.Name == "project logs" || command.Name == "doctor" {
					args = args[:1]
				}
				if _, err := parseRequest(command, args); err == nil || err.Details["field"] != "command" {
					t.Fatalf("%s accepted invalid key %q", command.Name, name)
				}
			}
		}
	}
}
