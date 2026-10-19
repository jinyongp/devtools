package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func bundleSchema() map[string]any {
	return object(map[string]any{"bundle_id": stringSchema(), "name": stringSchema(), "value_ids": arraySchema(stringSchema())}, "bundle_id", "name", "value_ids")
}
func (a *App) registerBundles() {
	a.commands = append(a.commands, Command{Name: "bundle list", Description: "List shared var/sec bundles and the current edit revision without unlocking values.", Options: profileOptions(false), Output: object(map[string]any{"profile": stringSchema(), "revision": stringSchema(), "integrity_verified": map[string]any{"type": "boolean"}, "items": arraySchema(bundleSchema())}, "profile", "revision", "integrity_verified", "items"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		store, e := a.store(r.Options)
		if e != nil {
			return nil, e
		}
		v, e := store.Inspect(ctx, "")
		if e != nil {
			return nil, e
		}
		return map[string]any{"profile": v.Profile, "revision": v.Revision, "integrity_verified": v.IntegrityVerified, "items": v.Bundles}, nil
	}})
	for _, action := range []string{"set", "rename", "remove"} {
		action := action
		options := append(profileOptions(false), Option{Name: "revision", Required: true, Pattern: "^[0-9a-f]{64}$", Description: "Edit revision from bundle list; stale edits fail atomically."}, Option{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "Retry-safe UUID; repeat identical input to replay."}, Option{Name: "dry-run", Boolean: true, Description: "Validate the entire edit without publishing it."})
		args := []Argument{{Name: "name", Required: true, Pattern: project.ProfilePattern}}
		if action == "set" {
			options = append(options, Option{Name: "key", Repeatable: true, Pattern: values.KeyPattern, Description: "Shared value name to include; omission selects an empty bundle."})
		}
		if action == "rename" {
			args = append(args, Argument{Name: "new-name", Required: true, Pattern: project.ProfilePattern})
		}
		a.commands = append(a.commands, Command{Name: "bundle " + action, Description: "Atomically " + action + " a shared value bundle; values and slot versions are preserved.", Options: options, Arguments: args, Output: object(map[string]any{"profile": stringSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}, "revision": stringSchema(), "bundle": stringSchema()}, "profile", "changed", "replayed", "revision", "bundle"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			store, e := a.store(r.Options)
			if e != nil {
				return nil, e
			}
			b := &values.BundleChange{Name: r.Args[0], Members: r.ListOptions["key"], Preview: r.Options["dry-run"] == "true"}
			if action == "rename" {
				b.NewName = r.Args[1]
			}
			return store.Apply(ctx, values.Change{Action: "bundle." + action, Bundle: b, Revision: r.Options["revision"], RequestID: r.Options["request-id"]})
		}})
	}
}
