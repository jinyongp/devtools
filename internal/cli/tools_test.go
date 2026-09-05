package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
