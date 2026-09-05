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
