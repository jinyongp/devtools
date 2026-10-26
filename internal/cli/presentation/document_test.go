package presentation

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestGenericPreservesPublicTypesAndUnknownFields(t *testing.T) {
	const public = `{"schema_version":1,"ok":true,"data":{"z":null,"a":false,"b":0,"c":"","big":18446744073709551615,"decimal":1.2300,"array":["second","first"],"new_field":{"state":"unrecognized"},"remedies":[]}}`
	doc, err := Build("new command", []byte(public))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cliui.Render(doc, cliui.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"false", "0", `""`, "null", "18446744073709551615", "1.2300", "new_field", "unrecognized", "remedies", "[]"} {
		if !strings.Contains(string(out), expected) {
			t.Errorf("missing %q: %s", expected, out)
		}
	}
	if strings.Index(string(out), "second") > strings.Index(string(out), "first") {
		t.Fatal("array order changed", string(out))
	}
	for _, invalid := range []string{`{"ok":true,"data":{}}`, `{"schema_version":2,"ok":true,"data":{}}`, public + `{}`, `{"schema_version":1,"ok":false,"error":{}}`, `{"schema_version":1,"ok":true,"data":{},"error":null}`} {
		if _, err := BuildGeneric([]byte(invalid)); err == nil {
			t.Errorf("invalid envelope accepted: %s", invalid)
		}
	}
}

func TestPublicActionsRetainRawArgvAndRequiredInputs(t *testing.T) {
	const public = `{"schema_version":1,"ok":true,"data":{"next_actions":[{"message":"Original action.","argv":["devtools","command","list","--dir","/workspace/my project"],"required_inputs":[],"effect":"read","new_metadata":"keep"},{"message":"Input still required.","argv":["devtools","task","show"],"required_inputs":["task-id"],"effect":"read"}]}}`
	doc, err := BuildGeneric([]byte(public))
	if err != nil {
		t.Fatal(err)
	}
	var commands [][]string
	var visit func([]cliui.Block)
	visit = func(blocks []cliui.Block) {
		for _, block := range blocks {
			if block.Kind() == cliui.CommandLineBlock {
				commands = append(commands, block.Argv())
			}
			visit(block.Children())
		}
	}
	visit(doc.Blocks())
	want := [][]string{{"devtools", "command", "list", "--dir", "/workspace/my project"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("action changed or template advertised as runnable: %#v", commands)
	}
	out, err := cliui.Render(doc, cliui.Options{CommandShells: []cliui.Shell{cliui.POSIX, cliui.Fish}})
	if err != nil || !strings.Contains(string(out), "Original action.") || !strings.Contains(string(out), "keep") || !strings.Contains(string(out), "task-id") || !strings.Contains(string(out), "Arguments (inputs required)") {
		t.Fatal(string(out), err)
	}
}
