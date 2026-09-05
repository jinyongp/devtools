package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorAndRunPreflight(t *testing.T) {
	app := testApp(t)
	root := t.TempDir()
	t.Chdir(root)
	config := `profile="app"
[requirements]
vars=["PORT"]
[requirements.tools.fixture]
executable="./tool"
version="1.2.3"
[commands.check]
exec=["/bin/sh","-c","test -n \"$TOKEN\" && printf ran > marker"]
inject=true
env="dev"
[commands.check.requirements]
secs=["TOKEN"]
`
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile("tool", []byte("#!/bin/sh\necho 'fixture 1.2.3 CANARY-SECRET'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	diagnose := func(want bool, args ...string) {
		t.Helper()
		code, out, err := invoke(t, app, "", append([]string{"doctor"}, args...)...)
		if code != 0 || err != "" {
			t.Fatal(code, out, err)
		}
		if strings.Contains(out, "CANARY-SECRET") {
			t.Fatal("probe output leaked")
		}
		var result struct{ Data struct{ Ready bool } }
		if e := json.Unmarshal([]byte(out), &result); e != nil || result.Data.Ready != want {
			t.Fatal(out, e)
		}
	}
	diagnose(false, "check")
	code, out, err := invoke(t, app, "", "run", "check")
	if code != 3 || out != "" {
		t.Fatal(code, out, err)
	}
	if _, e := os.Stat("marker"); !os.IsNotExist(e) {
		t.Fatal("ran without prerequisites")
	}
	for _, args := range [][]string{{"env", "create", "dev"}, {"var", "set", "PORT", "--value", "3000"}} {
		if code, _, err := invoke(t, app, "", args...); code != 0 {
			t.Fatal(err)
		}
	}
	if code, _, err := invoke(t, app, "CANARY-SECRET", "sec", "set", "TOKEN", "--env", "dev", "--stdin"); code != 0 {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if e := os.Mkdir(nested, 0700); e != nil {
		t.Fatal(e)
	}
	diagnose(true, "check", "--dir", nested)
	if code, out, err := invoke(t, app, "", "run", "check"); code != 0 || strings.Contains(out+err, "CANARY-SECRET") {
		t.Fatal(code, out, err)
	}
	if _, e := os.Stat("marker"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile("tool", []byte("#!/bin/sh\necho 'fixture 1.2.30 CANARY-SECRET'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	diagnose(false, "check")
	if code, out, err := invoke(t, app, "", "run", "check"); code != 3 || !strings.Contains(err, "requirements_failed") || strings.Contains(out+err, "CANARY-SECRET") {
		t.Fatal(code, out, err)
	}
}
func TestDoctorProfileOnlyAndFailures(t *testing.T) {
	app := testApp(t)
	root := t.TempDir()
	t.Chdir(root)
	code, out, err := invoke(t, app, "", "doctor", "--profile", "app")
	if code != 0 || !strings.Contains(out, `"ready":true`) {
		t.Fatal(out, err)
	}
	code, out, err = invoke(t, app, "", "doctor")
	if code != 0 || !strings.Contains(out, `"ready":false`) {
		t.Fatal(out, err)
	}
	if e := os.WriteFile("devtools.toml", []byte("invalid CANARY-SECRET"), 0600); e != nil {
		t.Fatal(e)
	}
	code, out, err = invoke(t, app, "", "doctor", "--profile", "app")
	if code != 0 || !strings.Contains(out, `"ready":false`) || strings.Contains(out+err, "CANARY-SECRET") {
		t.Fatal(out, err)
	}
	code, out, err = invoke(t, app, "", "doctor", "--dir", "missing")
	if code != 0 || !strings.Contains(out, `"id":"directory","status":"fail"`) {
		t.Fatal(out, err)
	}
}
