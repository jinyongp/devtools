package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortBindingsAndInstanceCLI(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	t.Chdir(root)
	config := `profile="app"
[ports.web]
range=[24000,24999]
[commands.test]
exec=["/bin/sh","-c","printf '%s|%s' \"$URL\" \"$1\"","--","${bind.P}"]
serve=["web"]
[commands.test.bind]
P={port="web"}
URL={template="http://${var.HOST}:${bind.P}/api"}
[commands.path]
exec=["${bind.EXE}"]
inject=true
[requirements.tools.true]
executable="true"
[commands.path.bind]
EXE={template="true"}
PATH={template="/bin:/usr/bin"}
`
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	call := func(args ...string) string {
		t.Helper()
		code, out, err := invoke(t, a, "", args...)
		if code != 0 {
			t.Fatal(code, out, err)
		}
		return out
	}
	call("var", "set", "HOST", "--value", "localhost")
	call("var", "set", "PATH", "--value", "/missing")
	if out := call("doctor", "path"); !strings.Contains(out, `"ready":true`) {
		t.Fatal(out)
	}
	call("run", "path")
	call("var", "unset", "PATH")
	if out := call("doctor", "test"); !strings.Contains(out, `"ready":true`) {
		t.Fatal(out)
	}
	call("instance", "name", "main")
	out := call("run", "test")
	if !strings.HasPrefix(out, "http://localhost:") || !strings.Contains(out, "/api|") {
		t.Fatal(out)
	}
	var response struct {
		Data struct {
			Port       int
			InstanceID string `json:"instance_id"`
		}
	}
	if e := json.Unmarshal([]byte(call("port", "show", "web")), &response); e != nil || response.Data.Port == 0 {
		t.Fatal(e, response)
	}
	if out := call("port", "allocate", "web"); !strings.Contains(out, `"created":false`) {
		t.Fatal(out)
	}
	if code, _, err := invoke(t, a, "secret-canary", "sec", "set", "URL", "--stdin"); code != 0 {
		t.Fatal(err)
	}
	code, out, err := invoke(t, a, "", "run", "test")
	if code != 3 || !strings.Contains(err, "binding_conflict") || strings.Contains(out+err, "secret-canary") {
		t.Fatal(code, out, err)
	}
	call("port", "release", "web")
	call("instance", "remove", "main")
	t.Chdir(filepath.Dir(root))
	call("port", "list", "--profile", "app")
	call("instance", "list", "--profile", "app")
}

func TestPortFailureExitCodes(t *testing.T) {
	if err := portFailure("io_error"); err.ExitCode != 1 {
		t.Fatal("I/O error exit code", err.ExitCode)
	}
	if err := portFailure("instance_not_found"); err.ExitCode != 3 {
		t.Fatal("condition error exit code", err.ExitCode)
	}
}

func TestInstancePositionalSchema(t *testing.T) {
	app := testApp(t)
	for _, args := range [][]string{{"instance", "name", "--help"}, {"instance", "move", "--help"}, {"instance", "remove", "--help"}} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 0 || diagnostic != "" || strings.Contains(out, "--instance") {
			t.Errorf("%v advertises conflicting --instance: exit=%d stdout=%s stderr=%s", args, code, out, diagnostic)
		}
		if args[1] == "move" && (!strings.Contains(out, "--dir VALUE") || !strings.Contains(out, "(required)")) {
			t.Errorf("move does not require --dir: %s", out)
		}
		if args[1] == "remove" && strings.Contains(out, "--dir") {
			t.Errorf("remove advertises unsupported --dir: %s", out)
		}
		if args[1] == "name" && !strings.Contains(out, "selected project execution location") {
			t.Errorf("name does not account for --dir selection: %s", out)
		}
	}
	code, out, diagnostic := invoke(t, app, "", "instance", "move", "main")
	if code != 2 || out != "" || !strings.Contains(diagnostic, `"field":"dir"`) {
		t.Errorf("move accepted missing --dir: exit=%d stdout=%s stderr=%s", code, out, diagnostic)
	}
}
