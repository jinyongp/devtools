package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/cleanup"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) registerCleanup() {
	descriptions := map[string]string{
		"preview":  "Preview recoverable storage cleanup.",
		"apply":    "Archive selected items from a cleanup preview.",
		"archives": "List recoverable cleanup archives.",
		"restore":  "Restore one cleanup archive.",
		"purge":    "Permanently remove one eligible cleanup archive payload.",
	}
	for _, action := range []string{"preview", "apply", "archives", "restore", "purge"} {
		entry := map[string]any{"type": "object"}
		items := map[string]any{"type": "array", "items": entry}
		output := object(map[string]any{"items": items}, "items")
		switch action {
		case "preview":
			output = object(map[string]any{"id": stringSchema(), "expires": stringSchema(), "items": items}, "id", "expires", "items")
		case "apply":
			output = object(map[string]any{"items": items, "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "items", "changed", "replayed")
		case "restore", "purge":
			output = changedItemOutput(entry)
		}
		c := Command{Name: "cleanup " + action, Description: descriptions[action], Options: []Option{}, Output: output}
		if action == "preview" {
			c.Options = profileOptions(false)
		}
		if action == "apply" {
			c.Arguments = []Argument{{Name: "plan-id", Required: true, Pattern: uuidPattern}}
			c.Options = []Option{{Name: "item", Repeatable: true, Required: true, Pattern: uuidPattern, Description: "Candidate UUID; repeat for selected candidates."}, {Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe application."}}
		}
		if action == "restore" || action == "purge" {
			c.Arguments = []Argument{{Name: "archive-id", Required: true, Pattern: uuidPattern}}
		}
		c.Run = func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			dirs, err := paths.Current()
			if err != nil {
				return nil, argumentError("Cannot resolve directories.", "")
			}
			d, e := a.dataDirectory()
			if e != nil {
				return nil, e
			}
			engine := cleanup.Engine{Data: filepath.Dir(d), Cache: dirs.Cache, Config: dirs.Config}
			switch action {
			case "preview":
				return engine.Preview(ctx, r.Options["profile"])
			case "apply":
				result, err := engine.Apply(ctx, r.Args[0], r.ListOptions["item"], r.Options["request-id"])
				return map[string]any{"items": append([]cleanup.Archive{}, result.Archives...), "changed": len(result.Archives) > 0, "replayed": result.Replayed}, err
			case "archives":
				items, e := engine.Archives()
				return map[string]any{"items": items}, e
			case "restore":
				item, changed, err := engine.Restore(ctx, r.Args[0])
				return map[string]any{"item": item, "changed": changed}, err
			case "purge":
				item, changed, err := engine.Purge(ctx, r.Args[0])
				return map[string]any{"item": item, "changed": changed}, err
			}
			return nil, nil
		}
		a.commands = append(a.commands, c)
	}
}
