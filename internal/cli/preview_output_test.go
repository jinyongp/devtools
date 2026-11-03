package cli

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestPreviewIntentFollowsApplyModeAndStrictAlternatives(t *testing.T) {
	for _, name := range []string{"backup restore", "profile import", "proxy teardown", "config migrate"} {
		for _, apply := range []bool{false, true} {
			schema := map[string]any{"oneOf": []any{object(map[string]any{"item": object(map[string]any{"digest": stringSchema()}, "digest")}, "item"), object(map[string]any{"item": object(map[string]any{"digest": stringSchema()}, "digest"), "changed": boolSchema()}, "item", "changed")}}
			c := Command{Name: name, Output: schema, Options: []Option{{Name: "request-id"}}, Run: func(_ context.Context, _ IO, r Request) (any, *protocol.Error) {
				value := map[string]any{"item": map[string]any{"digest": strings.Repeat("a", 64)}}
				if r.Options["apply"] != "" {
					value["changed"] = false
				}
				return value, nil
			}}
			previewResultContract(&c)
			options := map[string]string{}
			if apply {
				options["apply"] = strings.Repeat("a", 64)
			}
			value, e := c.Run(context.Background(), IO{}, Request{Options: options})
			if e != nil {
				t.Fatal(e)
			}
			out := value.(map[string]any)
			if out["dry_run"] != !apply {
				t.Fatal(name, apply, out)
			}
			if !apply {
				if len(out["next_actions"].([]protocol.Remedy)) != 1 {
					t.Fatal(out)
				}
			}
			for _, branch := range c.Output["oneOf"].([]any) {
				properties := branch.(map[string]any)["properties"].(map[string]any)
				if properties["dry_run"] == nil || properties["next_actions"] == nil {
					t.Fatal("strict alternative omitted invocation fields", name)
				}
			}
		}
	}
}

func TestPreviewApplyDoesNotEchoPrivateInputsOrChildArguments(t *testing.T) {
	c := Command{Name: "command add", Options: []Option{{Name: "identity-file"}, {Name: "stdin", Boolean: true}, {Name: "profile"}, {Name: "request-id"}}}
	r := Request{Args: []string{"web"}, Child: []string{"PROGRAM-PRIVATE-CANARY"}, Options: map[string]string{"identity-file": "PRIVATE-FILE-CANARY", "stdin": "true", "profile": "app"}, Invocation: protocol.InvocationContext{IsolatedRoot: "/chosen"}}
	action := previewApplyAction(c, r, map[string]any{})
	joined := strings.Join(action.Argv, " ")
	if strings.Contains(joined, "CANARY") || len(action.Argv) != 0 || action.State != "needs_input" || len(action.RequiredInputs) < 2 {
		t.Fatal(action)
	}
	if action.Bindings["profile"] != "app" || action.Bindings["isolated-root"] != "/chosen" || !regexp.MustCompile(uuidPattern).MatchString(action.Bindings["request-id"]) {
		t.Fatal("private-input acquisition lost its original public intent", action)
	}
}

func TestPreviewApplyPinsDirectoryAndRevisionForDeclarations(t *testing.T) {
	directory := t.TempDir()
	input, err := filepath.Abs("tool.json")
	if err != nil {
		t.Fatal(err)
	}
	c := Command{Name: "config tool add", Options: []Option{{Name: "dir"}, {Name: "revision"}, {Name: "file"}}}
	r := Request{Args: []string{"node"}, Options: map[string]string{"dir": ".", "file": "tool.json"}}
	action := previewApplyAction(c, r, map[string]any{"config_path": filepath.Join(directory, "devtools.toml"), "revision": strings.Repeat("a", 64)})
	for option, expected := range map[string]string{"--dir": directory, "--revision": strings.Repeat("a", 64), "--file": input} {
		found := false
		for i, arg := range action.Argv {
			if arg == option && i+1 < len(action.Argv) {
				found = action.Argv[i+1] == expected
				break
			}
		}
		if !found {
			t.Fatal("apply lost reviewed input scope", option, action)
		}
	}
}

func TestPreviewApplyPinsResolvedProfileWhenNoFlagWasProvided(t *testing.T) {
	c := Command{Name: "bundle set", Options: []Option{{Name: "profile"}, {Name: "revision"}, {Name: "request-id"}}}
	action := previewApplyAction(c, Request{Args: []string{"web"}, Options: map[string]string{}}, map[string]any{"profile": "resolved-app", "revision": strings.Repeat("a", 64)})
	if !strings.Contains(strings.Join(action.Argv, " "), "--profile resolved-app") {
		t.Fatal(action)
	}
}
