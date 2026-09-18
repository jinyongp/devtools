package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestOutputModeCatalog(t *testing.T) {
	app := New("test", "test")
	catalog := app.catalog()
	if catalog["protocol_version"] != protocol.ProtocolVersion || protocol.ProtocolVersion != 3 {
		t.Fatalf("unexpected CLI protocol version: %v", catalog["protocol_version"])
	}
	responseSchema := catalog["response_schema"].(map[string]any)
	properties := responseSchema["properties"].(map[string]any)
	if properties["schema_version"].(map[string]any)["const"] != protocol.EnvelopeVersion || protocol.EnvelopeVersion != 1 {
		t.Fatalf("unexpected envelope version: %#v", properties["schema_version"])
	}
	nonJSON := map[string]OutputMode{"help": OutputText, "completion": OutputArtifact, "command run": OutputPassthrough}
	for _, entry := range catalog["commands"].([]map[string]any) {
		name := entry["name"].(string)
		want := OutputJSON
		if mode, ok := nonJSON[name]; ok {
			want = mode
		}
		if entry["output_mode"] != want {
			t.Errorf("%s output_mode = %v, want %s", name, entry["output_mode"], want)
		}
		if _, old := entry["stream_output"]; old {
			t.Errorf("%s still uses the ambiguous stream_output field", name)
		}
		_, hasSchema := entry["output_schema"]
		if hasSchema != (want == OutputJSON) {
			t.Errorf("%s JSON schema presence = %t", name, hasSchema)
		}
		if strings.HasSuffix(name, " list") || name == "cleanup archives" {
			schema := entry["output_schema"].(map[string]any)
			if !slices.Contains(schema["required"].([]string), "items") {
				t.Errorf("%s does not require an items collection", name)
			}
		}
		for _, option := range entry["options"].([]Option) {
			if option.Name == "json" {
				t.Errorf("%s has a command-local output switch", name)
			}
		}
	}
	for _, args := range [][]string{{"schema", "command", "run"}, {"schema", "run"}} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 0 || diagnostic != "" || !strings.Contains(out, `"protocol_version":3`) || !strings.Contains(out, `"output_mode":"passthrough"`) || strings.Contains(out, `"output_schema"`) {
			t.Fatalf("%v: %d %s %s", args, code, out, diagnostic)
		}
	}
}

func TestVersionReportsMachineContractVersions(t *testing.T) {
	data := outputData(t, New("1.2.3", "abc123"), []string{"version"})
	if data["version"] != "1.2.3" || data["commit"] != "abc123" {
		t.Fatalf("unexpected build metadata: %#v", data)
	}
	if data["protocol_version"] != float64(protocol.ProtocolVersion) {
		t.Fatalf("unexpected protocol metadata: %#v", data)
	}
	if _, duplicated := data["schema_version"]; duplicated {
		t.Fatalf("envelope version should not be duplicated in data: %#v", data)
	}
}

func TestJSONQueryEnvelopes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Chdir(root)
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	for _, args := range [][]string{
		{"command", "list"}, {"env", "list"}, {"variable", "list"}, {"secret", "list"},
		{"port", "list"}, {"instance", "list"}, {"process", "list"}, {"proxy", "list"}, {"profile", "list"},
		{"task", "list"}, {"task", "workstream", "list"}, {"task", "validation", "list"},
		{"cleanup", "archives"},
	} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			data := outputData(t, app, args)
			items, ok := data["items"].([]any)
			if !ok || len(items) != 0 {
				t.Fatalf("expected an empty array, got %#v", data)
			}
			for _, key := range []string{"commands", "envs", "archives"} {
				if _, old := data[key]; old {
					t.Errorf("legacy list field %s", key)
				}
			}
		})
	}
	for _, args := range [][]string{{"project", "inspect"}, {"dashboard", "status"}, {"proxy", "status"}} {
		data := outputData(t, app, args)
		if _, ok := data["item"].(map[string]any); !ok {
			t.Fatalf("%v missing resource: %#v", args, data)
		}
	}
	stop := []string{"proxy", "stop", "--request-id", "c03a9da3-782d-4abc-8888-999999999999"}
	for _, replayed := range []bool{false, true} {
		data := outputData(t, app, stop)
		changed, hasChanged := data["changed"].(bool)
		gotReplay, hasReplay := data["replayed"].(bool)
		if !hasChanged || changed || !hasReplay || gotReplay != replayed {
			t.Fatalf("no-op/replay metadata: %#v", data)
		}
		item := data["item"].(map[string]any)
		if _, nested := item["changed"]; nested {
			t.Fatal("mutation metadata leaked into the resource")
		}
	}
	code, out, diagnostic := invoke(t, app, "", "dashboard", "--json")
	if code != 2 || out != "" || !json.Valid([]byte(diagnostic)) {
		t.Fatalf("removed output option: %d %s %s", code, out, diagnostic)
	}
}

func outputData(t *testing.T, app *App, args []string) map[string]any {
	t.Helper()
	code, out, diagnostic := invoke(t, app, "", args...)
	if code != 0 || diagnostic != "" {
		t.Fatalf("%v: %d %s %s", args, code, out, diagnostic)
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || !envelope.OK || envelope.Data == nil {
		t.Fatalf("%v: invalid envelope %s", args, out)
	}
	return envelope.Data
}

func TestNonJSONOutputBoundaries(t *testing.T) {
	t.Chdir(t.TempDir())
	config := "profile='app'\n[commands.fail]\nexec=['/bin/sh','-c','printf output; printf diagnostic >&2; exit 17']\n"
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		code, out, diagnostic := invoke(t, app, "", "completion", shell)
		if code != 0 || diagnostic != "" || out != app.completionScript(shell) {
			t.Fatalf("artifact %s: %d %s", shell, code, diagnostic)
		}
	}
	for _, args := range [][]string{{"command", "run", "fail"}, {"run", "fail"}} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 17 || out != "output" || diagnostic != "diagnostic" {
			t.Fatalf("passthrough %v: %d %q %q", args, code, out, diagnostic)
		}
	}
	canonical := outputData(t, app, []string{"schema", "command", "run"})
	alias := outputData(t, app, []string{"schema", "run"})
	if !reflect.DeepEqual(canonical, alias) {
		t.Fatal("alias has a different output contract")
	}
}

func TestOutputModeCannotBypassJSON(t *testing.T) {
	for _, tc := range []struct {
		mode OutputMode
		data any
	}{
		{OutputJSON, processResult{ExitCode: 0}},
		{OutputText, map[string]any{}},
		{OutputArtifact, "unexpected"},
		{OutputPassthrough, map[string]any{}},
	} {
		app := New("test", "test")
		app.commands = append(app.commands, Command{Name: "fixture", OutputMode: tc.mode, Run: func(context.Context, IO, Request) (any, *protocol.Error) { return tc.data, nil }})
		code, out, diagnostic := invoke(t, app, "", "fixture")
		if code != 1 || out != "" || !strings.Contains(diagnostic, `"code":"internal_error"`) {
			t.Fatalf("mode %s: %d %s %s", tc.mode, code, out, diagnostic)
		}
	}
}
