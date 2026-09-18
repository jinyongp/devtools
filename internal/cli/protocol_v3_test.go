package cli

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestProtocolV3DiscoveryContracts(t *testing.T) {
	app := testApp(t)
	if protocol.ProtocolVersion != 3 || protocol.EnvelopeVersion != 1 {
		t.Fatal("unexpected machine/envelope version")
	}
	for _, args := range [][]string{
		{"schema"}, {"schema", "--all"}, {"schema", "profile"},
		{"schema", "profile", "list"}, {"schema", "profile", "inspect"},
		{"schema", "profile", "diff"}, {"schema", "profile", "import"},
		{"schema", "backup", "status"}, {"schema", "doctor"},
		{"schema", "project", "up"}, {"schema", "project", "status"},
		{"schema", "project", "down"}, {"schema", "run"},
	} {
		data := outputData(t, app, args)
		if data["protocol_version"] != float64(3) {
			t.Fatalf("%v: missing protocol v3", args)
		}
	}
	doctor := outputData(t, app, []string{"schema", "doctor"})
	checks := doctor["output_schema"].(map[string]any)["properties"].(map[string]any)["checks"].(map[string]any)["items"].(map[string]any)
	props := checks["properties"].(map[string]any)
	if _, legacy := props["remedy"]; legacy {
		t.Fatal("legacy string remedy is still advertised")
	}
	edit := outputData(t, app, []string{"schema", "task", "workstream", "edit"})
	next := edit["output_schema"].(map[string]any)["properties"].(map[string]any)["next_actions"]
	if !reflect.DeepEqual(props["remedies"], next) {
		t.Fatal("doctor/task remedy schemas differ")
	}
}

func TestGeneratedConfigRemainsVersionless(t *testing.T) {
	t.Chdir(t.TempDir())
	app := testApp(t)
	outputData(t, app, []string{"init", "--profile", "app"})
	body, err := os.ReadFile("devtools.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"version", "schema_version", "protocol_version"} {
		if strings.Contains(string(body), name) {
			t.Fatalf("generated config contains %s", name)
		}
	}
	for _, field := range []string{"version", "schema_version"} {
		if err := os.WriteFile("devtools.toml", []byte("profile='app'\n"+field+"=1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		code, out, _ := invoke(t, app, "", "project", "inspect")
		if code == 0 || out != "" {
			t.Fatalf("unsupported config field %s accepted", field)
		}
	}
}

func TestProfileImportSchemaPreviewApplyPair(t *testing.T) {
	app := testApp(t)
	var command Command
	for _, c := range app.commands {
		if c.Name == "profile import" {
			command = c
		}
	}
	if command.Name == "" || len(command.InputOneOf) != 2 {
		t.Fatal("missing preview/apply variants")
	}
	for _, option := range command.Options {
		if slices.Contains([]string{"apply", "request-id"}, option.Name) && option.Required {
			t.Fatal("preview requires mutation input")
		}
	}
	data := outputData(t, app, []string{"schema", "profile", "import"})
	properties := data["output_schema"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"item", "digest", "target_exists", "diff", "changed", "replayed", "safety_backup"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("missing import output %s", key)
		}
	}
}
