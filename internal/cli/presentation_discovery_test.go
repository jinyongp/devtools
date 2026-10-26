package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cli/presentation"
)

func TestPresentationMetadataAndGlobalOptionPlacement(t *testing.T) {
	a := presentationApp(presentation.Environment{})
	catalog := a.catalog()
	global := catalog["global_options"].([]map[string]any)
	if len(global) != 2 || global[0]["name"] != "format" || global[0]["default"] != string(presentation.DefaultFormat) || global[0]["environment"] != "DEVTOOLS_FORMAT" || global[0]["placement"] != "root-prefix" {
		t.Fatal(global)
	}
	for _, command := range catalog["commands"].([]map[string]any) {
		if _, duplicate := command["global_options"]; duplicate {
			t.Fatal("duplicated global metadata in catalog entry", command["name"])
		}
		metadata := command["presentation"].(map[string]any)
		formats := metadata["formats"].([]string)
		if machineCommand(command["name"].(string)) {
			if !reflect.DeepEqual(formats, []string{"json"}) || metadata["default"] != "json" {
				t.Fatal(command)
			}
		} else if command["output_mode"] == OutputJSON {
			if !reflect.DeepEqual(formats, []string{"json", "text"}) || metadata["default"] != string(presentation.DefaultFormat) {
				t.Fatal(command)
			}
		} else if len(formats) != 0 || metadata["default"] != "native" {
			t.Fatal(command)
		}
		for _, option := range command["options"].([]Option) {
			if option.Name == "format" {
				t.Fatal("root format became command-local", command["name"])
			}
		}
	}
	for _, target := range []string{"", "command", "version", "run", "command run", "config schema"} {
		value, _, valid := a.discovery(target, true)
		if !valid || !reflect.DeepEqual(value.(map[string]any)["global_options"], global) {
			t.Fatal("missing global metadata", target, value)
		}
	}
	for _, target := range []string{"version", "schema", "config schema", "run"} {
		value, _, _ := a.discovery(target, true)
		input := value.(map[string]any)["input_schema"].(map[string]any)
		format := input["properties"].(map[string]any)["format"].(map[string]any)
		want := []string{"auto", "text", "json"}
		if machineCommand(target) {
			want = []string{"auto", "json"}
		}
		if !reflect.DeepEqual(format["enum"], want) {
			t.Fatal(target, format)
		}
	}
}

func TestRootCompletionUsesPrivateWireAndSelectedScope(t *testing.T) {
	a := New("test", "test")
	for _, tc := range []struct{ words, want []string }{
		{[]string{"--format", ""}, []string{"auto", "text", "json"}},
		{[]string{"--format", "t"}, []string{"text"}},
		{[]string{"--format=j"}, []string{"--format=json"}},
		{[]string{"--format", "json", "run", "child", "--format", ""}, nil},
		{[]string{"run", "--format", ""}, nil},
	} {
		if got := a.dynamicCandidates(tc.words); !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc.words, got, tc.want)
		}
	}
	a.observeOutput = func(IO) presentation.Environment {
		t.Fatal("private completion observed public format")
		return presentation.Environment{}
	}
	code, out, diagnostic := invoke(t, a, "--format\x00\x00", "__complete")
	if code != 0 || diagnostic != "" || out != "auto\ntext\njson\n" {
		t.Fatal(code, out, diagnostic)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "devtools.toml"), []byte("profile='fixture'\n[commands.dev]\nexec=['true']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(project, "uncreated-store")
	words := []string{"--format", "text", "--isolated-root", root, "command", "inspect", "--dir", project, ""}
	if got := a.dynamicCandidates(words); !reflect.DeepEqual(got, []string{"dev"}) {
		t.Fatal(got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("completion created a store", err)
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		script := a.completionScript(shell)
		if !strings.Contains(script, "--format") || !strings.Contains(script, "auto") || !strings.Contains(script, "text") || !strings.Contains(script, "json") {
			t.Fatal("missing static root enum", shell)
		}
	}
}
