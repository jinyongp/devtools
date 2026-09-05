package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/cleanup"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) registerCleanup() {
	for _, action := range []string{"preview", "apply", "archives", "restore", "purge"} {
		c := Command{Name: "cleanup " + action, Description: action + " selected storage retirement and recovery.", Options: []Option{}, Output: map[string]any{"type": "object"}}
		if action == "preview" {
			c.Options = profileOptions(false)
		}
		if action == "apply" {
			c.Arguments = []Argument{{Name: "plan", Required: true}}
			c.Options = []Option{{Name: "item", Repeatable: true, Required: true, MinLength: 36, MaxLength: 36, Description: "Candidate UUID; repeat for selected candidates."}, {Name: "request-id", Required: true, MinLength: 36, MaxLength: 36, Description: "UUID for retry-safe application."}}
		}
		if action == "restore" || action == "purge" {
			c.Arguments = []Argument{{Name: "archive", Required: true}}
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
				return engine.Apply(ctx, r.Args[0], r.ListOptions["item"], r.Options["request-id"])
			case "archives":
				items, e := engine.Archives()
				return map[string]any{"items": items}, e
			case "restore":
				return engine.Restore(ctx, r.Args[0])
			case "purge":
				return engine.Purge(ctx, r.Args[0])
			}
			return nil, nil
		}
		a.commands = append(a.commands, c)
	}
}
