package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkEnvironmentPrecedenceAndSinglePass(t *testing.T) {
	root := t.TempDir()
	config := `profile='app'
env_files=['.env.local','.env','.missing']
[ports.web]
port='${PORT:-0}'
[proxies.web]
host='${DOMAIN:-web.localhost}'
port='web'
[proxies.unselected]
host='${OTHER_REQUIRED}'
port='web'
`
	for file, data := range map[string]string{Filename: config, ".env.local": "DOMAIN=first.localhost\nPORT=31001\nPRIVATE=do-not-persist\n", ".env": "DOMAIN=last.localhost\nPORT=31002\n"} {
		if e := os.WriteFile(filepath.Join(root, file), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	p, e := Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := p.ResolveNetwork(nil, "", "web")
	if e != nil || resolved.Context.Proxies["web"].Host != "first.localhost" || *resolved.Context.Ports["web"].Port != 31001 {
		t.Fatal(resolved, e)
	}
	process, e := p.ResolveNetwork([]string{"DOMAIN=parent.localhost", "PORT="}, "", "web")
	if e != nil || process.Context.Proxies["web"].Host != "parent.localhost" || process.Context.Ports["web"].Port != nil {
		t.Fatal(process, e)
	}
	if process.SourceDigest != resolved.SourceDigest {
		t.Fatal("parent environment entered source digest")
	}
	if _, e := p.ResolveNetwork(nil, ""); e == nil || strings.Contains(e.Error(), "do-not-persist") {
		t.Fatal(e)
	}
	value, e := ExpandNetwork("${VALUE}", func(string) (string, bool) { return "${LITERAL}", true }, nil)
	if e != nil || value != "${LITERAL}" {
		t.Fatal(value, e)
	}
	if e := os.WriteFile(filepath.Join(root, ".env.local"), []byte("SECRET\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := p.ResolveNetwork(nil, "", "web"); e == nil || e.Details["line"] == nil || e.Details["file"] != ".env.local" || strings.Contains(e.Error(), "SECRET") {
		t.Fatal(e)
	}
}
func TestNetworkPortFormsAndSanitizedErrors(t *testing.T) {
	for _, test := range []struct {
		value  string
		strict bool
		valid  bool
		port   int
	}{{"3000", false, true, 3000}, {"0", false, true, 0}, {"", false, true, 0}, {"0", true, false, 0}, {"70000", false, false, 0}, {"$(echo private)", false, false, 0}} {
		root := t.TempDir()
		raw := "profile='app'\n[ports.web]\nport='" + test.value + "'\n"
		if test.strict {
			raw += "strict=true\n"
		}
		p, e := parse([]byte(raw), "", root)
		if e != nil {
			t.Fatal(e)
		}
		resolved, e := p.ResolveNetwork(nil, "")
		if (e == nil) != test.valid {
			t.Fatalf("%+v: %v", test, e)
		}
		if e == nil && test.port != 0 && *resolved.Context.Ports["web"].Port != test.port {
			t.Fatal(resolved)
		}
	}
}
