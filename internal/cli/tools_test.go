package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func testApp(t *testing.T) *App {
	t.Helper()
	app := New("test", "test")
	directory := filepath.Join(t.TempDir(), "profiles")
	app.dataDirectory = func() (string, *protocol.Error) { return directory, nil }
	return app
}

func invoke(t *testing.T, app *App, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := app.Run(context.Background(), args, IO{In: strings.NewReader(input), Out: &out, Err: &diagnostic})
	return code, out.String(), diagnostic.String()
}

func TestValueCommands(t *testing.T) {
	app := testApp(t)
	profile := []string{"--profile", "app"}
	call := func(want int, input string, args ...string) string {
		t.Helper()
		code, out, diagnostic := invoke(t, app, input, append(args, profile...)...)
		if code != want {
			t.Fatalf("%v: exit=%d out=%s err=%s", args, code, out, diagnostic)
		}
		if strings.Contains(out+diagnostic, "CANARY-SECRET") {
			t.Fatal("secret leaked in CLI response")
		}
		if code != 0 && out != "" {
			t.Fatal("error wrote stdout")
		}
		return out + diagnostic
	}
	call(0, "", "env", "create", "local")
	call(0, "", "env", "create", "local")
	call(0, "", "var", "set", "LEVEL", "--value", "info")
	call(0, "", "variable", "set", "LEVEL", "--env", "local", "--value", "debug")
	if output := call(0, "", "var", "get", "LEVEL", "--env", "local"); !strings.Contains(output, `"value":"debug"`) {
		t.Fatal(output)
	}
	call(0, "CANARY-SECRET\n", "sec", "set", "TOKEN", "--stdin")
	call(0, "", "secret", "list", "--env", "local")
	call(3, "", "var", "get", "TOKEN")
	call(3, "", "var", "set", "TOKEN", "--value", "oops")
	call(2, "", "sec", "set", "TOKEN", "--value", "CANARY-SECRET")
	call(2, "", "sec", "set", "TOKEN")
	call(2, "", "sec", "set", "TOKEN", "--stdin", "--file", "unused")
	call(3, "", "env", "remove", "local")
	call(0, "", "var", "unset", "LEVEL", "--env", "local")
	if output := call(0, "", "var", "get", "LEVEL", "--env", "local"); !strings.Contains(output, `"value":"info"`) {
		t.Fatal(output)
	}
	call(0, "", "env", "remove", "local")
	call(3, "", "var", "list", "--env", "local")
	call(3, "", "var", "set", "LEVEL", "--env", "typo", "--value", "bad")
	call(2, "", "var", "list", "--common")
	call(2, "", "var", "list", "--profile", "duplicate")
}

func TestNamedAndDirectRun(t *testing.T) {
	app := testApp(t)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("LEVEL", "parent")
	config := `profile = "app"
[commands.check]
exec = ["/bin/sh", "-c", "printf '%s|%s|%s' \"$PWD\" \"$LEVEL\" \"$1\"", "label"]
inject = true
env = "local"
[commands.plain]
exec = ["/bin/sh", "-c", "printf '%s' \"$LEVEL\""]
[commands.fail]
exec = ["/bin/sh", "-c", "printf raw; printf diagnostic >&2; exit 23"]
`
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"env", "create", "local"}, {"env", "create", "staging"},
		{"var", "set", "LEVEL", "--value", "common"},
		{"var", "set", "LEVEL", "--env", "local", "--value", "local"},
		{"var", "set", "LEVEL", "--env", "staging", "--value", "staging"},
	} {
		if code, _, err := invoke(t, app, "", args...); code != 0 {
			t.Fatal(err)
		}
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	current, cwdErr := os.Getwd()
	if cwdErr != nil {
		t.Fatal(cwdErr)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args       []string
		want       string
		exit       int
		diagnostic string
	}{
		{[]string{"run", "check", "--", "extra space"}, canonical + "|local|extra space", 0, ""},
		{[]string{"run", "check", "--env", "staging", "--", "--watch"}, canonical + "|staging|--watch", 0, ""},
		{[]string{"run", "plain"}, "parent", 0, ""},
		{[]string{"run", "plain", "--env", "staging"}, "staging", 0, ""},
		{[]string{"run", "--", "/bin/sh", "-c", "printf '%s|%s' \"$PWD\" \"$LEVEL\""}, current + "|common", 0, ""},
		{[]string{"run", "fail"}, "raw", 23, "diagnostic"},
	} {
		code, out, diagnostic := invoke(t, app, "", tc.args...)
		if code != tc.exit || out != tc.want || diagnostic != tc.diagnostic {
			t.Fatalf("%v: %d %q %q", tc.args, code, out, diagnostic)
		}
	}
	if code, out, diagnostic := invoke(t, app, "", "run", "plain", "--env", "missing"); code != 3 || out != "" || !strings.Contains(diagnostic, "env_not_found") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
	if os.Getenv("LEVEL") != "parent" {
		t.Fatal("changed parent environment")
	}
}

func TestSecretFileAndRawOutput(t *testing.T) {
	app := testApp(t)
	root := t.TempDir()
	file := filepath.Join(root, "secret")
	if err := os.WriteFile(file, []byte("true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := invoke(t, app, "", "sec", "set", "TOKEN", "--file", file, "--profile", "app"); code != 0 {
		t.Fatal(diagnostic)
	}
	code, out, diagnostic := invoke(t, app, "", "run", "--profile", "app", "--", "/bin/sh", "-c", "printf '%s' \"$TOKEN\"; printf true")
	if code != 0 || out != "true\ntrue" || diagnostic != "" {
		t.Fatalf("%d %q %s", code, out, diagnostic)
	}
}

func TestSchemaDescribesAliasesAndInputs(t *testing.T) {
	_, out, _ := invoke(t, testApp(t), "", "schema")
	var result struct {
		Data struct {
			Commands []struct {
				Name    string         `json:"name"`
				Aliases []string       `json:"aliases"`
				Input   map[string]any `json:"input_schema"`
			} `json:"commands"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, command := range result.Data.Commands {
		if command.Name == "secret set" {
			found = true
			if len(command.Aliases) != 1 || command.Aliases[0] != "sec set" || command.Input["oneOf"] == nil {
				t.Fatalf("%+v", command)
			}
		}
		if command.Name == "secret get" {
			t.Fatal("secret read command exposed")
		}
	}
	if !found {
		t.Fatal("secret set schema missing")
	}
}

func TestSecretInputCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := readSecret(ctx, IO{In: reader}, map[string]string{"stdin": "true"})
	if err == nil || err.Code != "canceled" {
		t.Fatal(err)
	}
}

func TestNamedProfileOverrideAndNoInjection(t *testing.T) {
	app := testApp(t)
	root := t.TempDir()
	t.Chdir(root)
	config := "profile='base'\n[commands.show]\nexec=['/bin/sh','-c','printf %s \"$MARKER\"']\ninject=true\n[commands.plain]\nexec=['/bin/sh','-c','printf plain']\ninject=false\n"
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, diagnostic := invoke(t, app, "", "var", "set", "MARKER", "--profile", "other", "--value", "override")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	code, out, diagnostic := invoke(t, app, "", "run", "show", "--profile", "other")
	if code != 0 || out != "override" || diagnostic != "" {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
	app.dataDirectory = func() (string, *protocol.Error) { t.Error("plain command accessed value storage"); return "", nil }
	code, out, diagnostic = invoke(t, app, "", "run", "plain")
	if code != 0 || out != "plain" || diagnostic != "" {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
}
