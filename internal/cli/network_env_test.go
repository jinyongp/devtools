package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandEnvIsPublicAndDoesNotAllocate(t *testing.T) {
	a := testApp(t)
	root := privateTempDir(t)
	t.Chdir(root)
	config := `profile='app'
[ports.api]
range=[31000,31999]
[proxies.api]
host='api.localhost'
port='api'
[commands.web]
exec=['true']
serve=['api']
[commands.web.bind]
PORT={port='api'}
API_URL={port='api',format='url'}
PUBLIC_URL={proxy='api'}
PRIVATE={template='${var.CANARY}'}
`
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	directory, _ := a.dataDirectory()
	registry := filepath.Join(filepath.Dir(directory), "ports", "registry.json")
	code, out, err := invoke(t, a, "", "command", "env", "web")
	if code != 0 || !strings.Contains(out, `"env":{}`) || !strings.Contains(out, "instance_not_found") {
		t.Fatal(code, out, err)
	}
	if _, e := os.Stat(registry); !os.IsNotExist(e) {
		t.Fatal("read-only env allocated registry", e)
	}
	for _, args := range [][]string{{"port", "allocate", "api"}, {"var", "set", "CANARY", "--value", "private-canary"}} {
		if code, _, err := invoke(t, a, "", args...); code != 0 {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(registry)
	code, out, err = invoke(t, a, "", "command", "env", "web")
	after, _ := os.ReadFile(registry)
	if code != 0 || !strings.Contains(out, "http://127.0.0.1:") || !strings.Contains(out, "http://api.localhost:20200") || strings.Contains(out, "private-canary") || string(before) != string(after) {
		t.Fatal(code, out, err)
	}
	if code, _, err := invoke(t, a, "secret-canary", "sec", "set", "PORT", "--stdin"); code != 0 {
		t.Fatal(err)
	}
	code, out, err = invoke(t, a, "", "command", "env", "web")
	if code != 0 || !strings.Contains(out, "binding_conflict") || strings.Contains(out, "secret-canary") {
		t.Fatal(code, out, err)
	}
}
