package cli

import (
	"context"
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
	if strings.Contains(joined, "CANARY") || !strings.Contains(joined, "--profile app") || !strings.Contains(joined, "--isolated-root /chosen") || len(action.RequiredInputs) < 3 {
		t.Fatal(action)
	}
}
