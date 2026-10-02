package cli

import (
	"os"
	"strings"
	"testing"
)

func TestRunCommandArgumentBoundary(t *testing.T) {
	app := testApp(t)
	t.Chdir(privateTempDir(t))
	config := `profile = "app"
[commands."dev:docs"]
exec = ["/bin/sh", "-c", "printf '%s|' \"$LEVEL\"; printf '<%s>' \"$@\"", "label"]
inject = true
env = "local"
`
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"env", "create", "local"}, {"env", "create", "staging"},
		{"var", "set", "LEVEL", "--env", "local", "--value", "local"},
		{"var", "set", "LEVEL", "--env", "staging", "--value", "staging"},
	} {
		if code, _, diagnostic := invoke(t, app, "", args...); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	for _, prefix := range [][]string{{"run"}, {"command", "run"}, {"cmd", "run"}} {
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"dev:docs", "--watch", "extra space", "", "문서", "--env", "missing", "--profile=missing", "--dir", "missing", "--help", "-h", "--", "tail"},
				"local|<--watch><extra space><><문서><--env><missing><--profile=missing><--dir><missing><--help><-h><--><tail>"},
			{[]string{"--env", "staging", "dev:docs", "--watch"}, "staging|<--watch>"},
			{[]string{"--env=staging", "dev:docs", "file"}, "staging|<file>"},
			{[]string{"dev:docs", "--help"}, "local|<--help>"},
			{[]string{"dev:docs", "-h"}, "local|<-h>"},
			{[]string{"dev:docs", "--", "--watch"}, "local|<--watch>"},
			{[]string{"dev:docs", "--", "--", "--help"}, "local|<--><--help>"},
			{[]string{"dev:docs", "file", "--", "--env", "staging"}, "local|<file><--><--env><staging>"},
		} {
			args := append(append([]string{}, prefix...), tc.args...)
			code, out, diagnostic := invoke(t, app, "", args...)
			if code != 0 || out != tc.want || diagnostic != "" {
				t.Fatalf("%v: code=%d out=%q err=%q want=%q", args, code, out, diagnostic, tc.want)
			}
		}
		for _, flag := range []string{"--help", "-h"} {
			args := append(append([]string{}, prefix...), flag)
			code, out, diagnostic := invoke(t, app, "", args...)
			if code != 0 || diagnostic != "" || !strings.Contains(out, "Usage: devtools command run [options] [command] [args...]") {
				t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, diagnostic)
			}
		}
	}
	for _, args := range [][]string{
		{"run", "--unknown", "dev:docs"},
		{"run", "--env"},
		{"run", "--env", "missing", "dev:docs"},
	} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code == 0 || out != "" || diagnostic == "" {
			t.Fatalf("invalid prefix %v: %d %q %q", args, code, out, diagnostic)
		}
	}
	// Other commands still accept their devtools options after positional args.
	data := outputData(t, app, []string{"var", "get", "LEVEL", "--env", "staging"})
	if data["value"] != "staging" {
		t.Fatalf("variable options stopped working after the key: %#v", data)
	}
	for _, command := range app.commands {
		if command.Name == "process start" {
			request, err := parseRequest(command, []string{"dev:docs", "--env", "staging", "--request-id", "00000000-0000-0000-0000-000000000001"})
			if err != nil || request.Options["env"] != "staging" || len(request.Child) != 0 {
				t.Fatalf("process start parsing changed: %#v %v", request, err)
			}
		}
	}
	schema := outputData(t, app, []string{"schema", "run"})
	input := schema["input_schema"].(map[string]any)["properties"].(map[string]any)
	child := input["child_args"].(map[string]any)
	if !strings.Contains(child["description"].(string), "devtools options precede the name") {
		t.Fatalf("run schema omits the argument boundary: %#v", child)
	}
}
