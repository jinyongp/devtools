package cli

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/jinyongp/devtools/internal/cleanup"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) registerCleanup() {
	maintenanceItem := object(map[string]any{"id": stringSchema(), "kind": stringSchema(), "reason": stringSchema(), "eligible": boolSchema(), "changed": boolSchema()}, "id", "kind", "reason", "eligible", "changed")
	count := map[string]any{"type": "integer", "minimum": 0}
	maintenanceOutput := object(map[string]any{"items": arraySchema(maintenanceItem), "examined": count, "changed": count, "reclaimed_bodies": count, "has_more": boolSchema(), "dry_run": boolSchema(), "verification_pending": boolSchema()}, "items", "examined", "changed", "reclaimed_bodies", "has_more", "dry_run", "verification_pending")
	a.commands = append(a.commands, Command{Name: "cleanup maintain", Description: "Inspect or run a bounded pass of automatic artifact retention; does not purge manual archives or user tasks.", Options: []Option{{Name: "profile", Pattern: project.ProfilePattern, Description: "Restrict maintenance to one profile."}, {Name: "limit", Default: "32", Pattern: positiveIntegerPattern, Description: "Maximum owned artifacts examined, from 1 to 200."}, {Name: "dry-run", Boolean: true, Description: "Report eligibility without changing artifacts or the saved position."}}, Output: maintenanceOutput, Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		dirs, err := a.currentPaths()
		if err != nil {
			return nil, argumentError("Cannot resolve directories.", "")
		}
		dir, condition := a.dataDirectory()
		if condition != nil {
			return nil, condition
		}
		if filepath.Clean(dirs.Data) != filepath.Clean(filepath.Dir(dir)) {
			dirs.Cache = ""
		}
		limit, err := strconv.Atoi(r.Options["limit"])
		if err != nil || limit < 1 || limit > 200 {
			return nil, argumentError("--limit must be between 1 and 200.", "cleanup maintain")
		}
		return (cleanup.Engine{Data: filepath.Dir(dir), Cache: dirs.Cache, Config: dirs.Config}).Maintain(ctx, r.Options["profile"], limit, r.Options["dry-run"] == "true")
	}})
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
			bucket := object(map[string]any{"kind": stringSchema(), "lifecycle": stringSchema(), "scope": map[string]any{"enum": []string{"profile", "root"}}, "verification": stringSchema(), "cleanup_eligible": boolSchema(), "reason": stringSchema(), "files": map[string]any{"type": "integer", "minimum": 0}, "logical_items": map[string]any{"type": "integer", "minimum": 0}, "bytes": map[string]any{"type": "integer", "minimum": 0}}, "kind", "lifecycle", "scope", "verification", "cleanup_eligible", "reason", "files", "logical_items", "bytes")
			inventory := object(map[string]any{"verification": map[string]any{"enum": []string{"verified", "verification_pending"}}, "profile": stringSchema(), "buckets": arraySchema(bucket)}, "verification")
			output = object(map[string]any{"id": stringSchema(), "expires": stringSchema(), "items": items, "retention": inventory}, "id", "expires", "items", "retention")
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
			dirs, err := a.currentPaths()
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
