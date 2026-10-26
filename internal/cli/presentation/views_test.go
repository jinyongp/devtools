package presentation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func publicDocument(t *testing.T, command string, data any) cliui.Document {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"schema_version": 1, "ok": true, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Build(command, encoded)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestCommandListIsAnInventoryWithUsefulSettings(t *testing.T) {
	longArg := strings.Repeat("long", 20)
	data := map[string]any{"profile": "demo", "next_cursor": "cursor-do-not-truncate", "new_root": "new-root-value", "warnings": []any{map[string]any{"code": "fixture", "message": "Warning must remain."}}, "items": []any{
		map[string]any{"name": "dev", "env": "local", "inject": true, "exec": []string{"pnpm", "dev"}, "serve": []string{}, "output": map[string]any{"mode": "auto"}, "values_update": "notify"},
		map[string]any{"name": "build", "env": "", "inject": false, "exec": []string{"pnpm", longArg}, "serve": []string{}, "new_row": map[string]any{"value": false}},
	}}
	doc := publicDocument(t, "command list", data)
	for _, width := range []int{40, 80, 120} {
		out, err := cliui.Render(doc, cliui.Options{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"Commands (2)", "demo", "dev", "build", "values: local", longArg, "cursor-do-not-truncate", "Warning must remain."} {
			if !strings.Contains(string(out), value) {
				t.Errorf("width %d lost %q: %s", width, value, out)
			}
		}
		for _, noise := range []string{"values_update", "selection", "Details", "Item 1", "argv tokens", "Environment", "Off", "new_row", "new_root"} {
			if strings.Contains(string(out), noise) {
				t.Fatalf("inventory contains configuration/debug noise %q: %s", noise, out)
			}
		}
	}
	// Changing a required type invalidates the entire dedicated view. Unknown
	// data is still shown by generic rendering, rather than silently dropping a row.
	data["items"].([]any)[1].(map[string]any)["inject"] = "unexpected-type"
	out, _ := cliui.Render(publicDocument(t, "command list", data), cliui.Options{})
	if !strings.HasPrefix(string(out), "Result\n") || !strings.Contains(string(out), "unexpected-type") || !strings.Contains(string(out), longArg) {
		t.Fatal("incompatible shape was partially summarized", string(out))
	}
}

func TestReadViewsEmptyValuesAndDefinitionOwnership(t *testing.T) {
	d := DefinitionFor("command list")
	d.Required[0].Path = "changed"
	d.Columns[0].Label = "changed"
	if DefinitionFor("command list").Required[0].Path == "changed" || DefinitionFor("command list").Columns[0].Label == "changed" {
		t.Fatal("definition exposed shared mutable storage")
	}
	for _, tc := range []struct {
		command  string
		data     any
		contains []string
	}{
		{"env list", map[string]any{"profile": "demo", "items": []string{}}, []string{"Environments (0)", "No items.", "demo"}},
		{"env list", map[string]any{"profile": "demo", "items": []string{"z", "a"}}, []string{"Environments (2)", "Environment", "z", "a"}},
		{"variable get", map[string]any{"profile": "demo", "value": "", "metadata": map[string]any{"key": "EMPTY", "source": "common", "overrides": false}, "integrity_verified": false}, []string{"Variable", "EMPTY", `""`, "false"}},
		{"process logs", map[string]any{"id": "log-id", "content": "first\nsecond\x1b[31m"}, []string{"Process logs", "log-id", "first\n", "second", `\u001b[31m`}},
		{"new list", map[string]any{"items": []any{}, "state": "UNKNOWN_NEW_STATE"}, []string{"Result", "UNKNOWN_NEW_STATE", "[]"}},
		{"version", map[string]any{"version": "test", "commit": "abc", "protocol_version": 14}, []string{"Version", "test", "abc", "14"}},
	} {
		out, err := cliui.Render(publicDocument(t, tc.command, tc.data), cliui.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range tc.contains {
			if !strings.Contains(string(out), expected) {
				t.Errorf("%s missing %q: %s", tc.command, expected, out)
			}
		}
	}
}

func TestKnownOmissionsNeverHideNewNestedData(t *testing.T) {
	base := map[string]any{"profile": "demo", "item": map[string]any{
		"name": "dev", "exec": []string{"pnpm", "dev"}, "env": "", "inject": true,
		"serve": []string{}, "bind": map[string]any{}, "ready": nil,
		"output":       map[string]any{"mode": "auto", "future_output": false},
		"requirements": map[string]any{"tools": map[string]any{}, "vars": []any{}, "secs": []any{}, "future_requirement": 0},
	}}
	out, err := cliui.Render(publicDocument(t, "command inspect", base), cliui.Options{})
	if err != nil || !strings.Contains(string(out), "future_output") || !strings.Contains(string(out), "future_requirement") || !strings.Contains(string(out), "false") || !strings.Contains(string(out), "0") {
		t.Fatal(string(out), err)
	}
}
