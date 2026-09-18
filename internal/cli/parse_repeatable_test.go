package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
)

func TestRepeatableFinalArgumentParsingAndDiscovery(t *testing.T) {
	command := Command{
		Name:        "repeat",
		Aliases:     []string{},
		Description: "Repeat commands.",
		Arguments:   []Argument{{Name: "command", Required: true, Repeatable: true, Pattern: project.ProfilePattern}},
		UniqueArgs:  true,
		Options:     []Option{},
		OutputMode:  OutputJSON,
		Output:      object(map[string]any{"items": map[string]any{"type": "array"}}, "items"),
	}
	request, err := parseRequest(command, []string{"web", "api", "worker"})
	if err != nil || !reflect.DeepEqual(request.Args, []string{"web", "api", "worker"}) {
		t.Fatalf("repeatable positional parse failed: %#v %v", request.Args, err)
	}
	if _, err := parseRequest(command, nil); err == nil || err.Code != "invalid_argument" || err.Details["field"] != "command" {
		t.Fatalf("missing repeated argument was accepted: %#v", err)
	}
	if _, err := parseRequest(command, []string{"web", "bad/value"}); err == nil || err.Code != "invalid_argument" || err.Details["field"] != "command" {
		t.Fatalf("invalid repeated argument was accepted: %#v", err)
	}
	if _, err := parseRequest(command, []string{"web", "web"}); err == nil || err.Code != "invalid_argument" {
		t.Fatalf("duplicate unique arguments were accepted: %#v", err)
	}
	invalid := command
	invalid.Arguments = []Argument{{Name: "first", Repeatable: true}, {Name: "second"}}
	if _, err := parseRequest(invalid, []string{"one", "two"}); err == nil || err.Code != "internal_error" {
		t.Fatalf("non-final repeatable argument was accepted: %#v", err)
	}

	app := New("test", "test")
	app.commands = append(app.commands, command)
	schema, _, ok := app.discovery("repeat", true)
	if !ok {
		t.Fatal("repeat schema not discovered")
	}
	input := schema.(map[string]any)["input_schema"].(map[string]any)
	args := input["properties"].(map[string]any)["args"].(map[string]any)
	if args["minItems"] != 1 || args["maxItems"] != nil || args["items"] == nil || args["uniqueItems"] != true {
		t.Fatalf("repeatable argument schema is not unbounded and unique: %#v", args)
	}
	if _, help, ok := app.discovery("repeat", false); !ok || !strings.Contains(help, "Usage: devtools repeat <command...>") {
		t.Fatalf("repeatable help is incorrect: %q", help)
	}
}

func TestOptionalRepeatableFinalArgumentAllowsZero(t *testing.T) {
	command := Command{Name: "repeat", Arguments: []Argument{{Name: "command", Repeatable: true, Pattern: project.ProfilePattern}}}
	request, err := parseRequest(command, nil)
	if err != nil || len(request.Args) != 0 {
		t.Fatalf("optional repeatable argument rejected zero values: %#v %v", request.Args, err)
	}
	request, err = parseRequest(command, []string{"web", "api"})
	if err != nil || !reflect.DeepEqual(request.Args, []string{"web", "api"}) {
		t.Fatalf("optional repeatable argument rejected values: %#v %v", request.Args, err)
	}
}
