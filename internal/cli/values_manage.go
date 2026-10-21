package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/protocol"
)

func guardMutation(command *Command, createOnly bool) {
	command.Options = append(command.Options,
		Option{Name: "revision", Pattern: "^[0-9a-f]{64}$", Description: "Expected profile edit revision from values revision; requires --request-id."},
		Option{Name: "request-id", Pattern: uuidPattern, Description: "Retry UUID paired with --revision; identical input replays the original result."})
	absent := []map[string]any{{"required": []string{"revision"}}, {"required": []string{"request-id"}}}
	if createOnly {
		command.Options = append(command.Options, Option{Name: "create-only", Boolean: true, Description: "Reject an existing exact-layer slot, including expired values; requires both guards."})
		absent = append(absent, map[string]any{"required": []string{"create-only"}})
	}
	command.InputAllOf = append(command.InputAllOf, map[string]any{"oneOf": []map[string]any{
		{"required": []string{"revision", "request-id"}},
		{"not": map[string]any{"anyOf": absent}},
	}})
	properties := command.Output["properties"].(map[string]any)
	properties["revision"] = stringSchema()
	properties["replayed"] = map[string]any{"type": "boolean"}
}

// This matches the schema constraint before a writer consumes file/stdin input.
func mutationGuard(options map[string]string) (bool, *protocol.Error) {
	_, revision := options["revision"]
	_, requestID := options["request-id"]
	_, createOnly := options["create-only"]
	if revision != requestID || createOnly && !revision {
		return false, argumentError("Provide --revision and --request-id together; --create-only requires both.", "")
	}
	return revision, nil
}

func managedChangeResultSchema() map[string]any {
	item := object(map[string]any{"key": stringSchema(), "kind": map[string]any{"enum": []string{"variable", "secret"}}, "action": stringSchema()}, "key", "kind", "action")
	return object(map[string]any{
		"profile": stringSchema(), "changed": map[string]any{"type": "boolean"},
		"revision": stringSchema(), "replayed": map[string]any{"type": "boolean"},
		"bundle": stringSchema(), "warnings": warningsSchema(), "items": arraySchema(item),
		"applicable": map[string]any{"type": "boolean"},
	}, "profile", "changed", "revision", "replayed")
}

func (a *App) registerValueManagement() {
	a.commands = append(a.commands, Command{Name: "values revision", Description: "Read the profile edit revision without values or unlocking; this token is not permission.", Options: profileOptions(true), Output: object(map[string]any{"profile": stringSchema(), "env": stringSchema(), "revision": stringSchema(), "integrity_verified": map[string]any{"const": false}}, "profile", "env", "revision", "integrity_verified"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		store, e := a.store(r.Options)
		if e != nil {
			return nil, e
		}
		view, e := store.Inspect(ctx, r.Options["env"])
		if e != nil {
			return nil, e
		}
		return map[string]any{"profile": view.Profile, "env": view.Env, "revision": view.Revision, "integrity_verified": false}, nil
	}})
	timestamp := map[string]any{"type": []string{"string", "null"}, "format": "date-time"}
	item := object(map[string]any{"request_id": stringSchema(), "state": map[string]any{"enum": []string{"completed", "expired"}}, "result": managedChangeResultSchema(), "created_at": timestamp, "expires_at": timestamp}, "request_id", "state", "created_at", "expires_at")
	a.commands = append(a.commands, Command{Name: "values request show", Description: "Read saved operation metadata using an existing warm session; never executes a missing request.", Options: profileOptions(false), Arguments: []Argument{{Name: "request-id", Required: true, Pattern: uuidPattern}}, Output: object(map[string]any{"profile": stringSchema(), "item": item}, "profile", "item"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		store, e := a.store(r.Options)
		if e != nil {
			return nil, e
		}
		return store.InspectRequest(ctx, r.Args[0])
	}})
}
