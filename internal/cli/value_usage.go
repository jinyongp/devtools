package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func inputVersionSchema() map[string]any {
	spec := object(map[string]any{"selection": stringSchema(), "inject": map[string]any{"type": "boolean"}, "bindings": arraySchema(stringSchema()), "shadowed": arraySchema(stringSchema())}, "selection", "inject", "bindings", "shadowed")
	slot := object(map[string]any{"kind": map[string]any{"enum": []string{"variable", "secret"}}, "env": stringSchema(), "name": stringSchema(), "value_id": stringSchema(), "slot_id": stringSchema(), "value_version": stringSchema()}, "name", "value_id", "slot_id", "value_version")
	identity := object(map[string]any{"profile_id": stringSchema(), "storage_generation": stringSchema(), "effective_input_revision": stringSchema()}, "profile_id", "storage_generation", "effective_input_revision")
	v := object(map[string]any{"identity": identity, "spec": spec, "env": stringSchema(), "slots": arraySchema(slot), "integrity_verified": map[string]any{"type": "boolean"}, "key_epoch": map[string]any{"type": "integer", "minimum": 0}}, "identity", "spec", "env", "slots", "integrity_verified")
	v["type"] = []string{"object", "null"}
	return v
}
func valueWatchSchema() map[string]any {
	return object(map[string]any{
		"state":  map[string]any{"enum": []string{"current", "pending", "failed", "unknown", "verification_pending"}},
		"reason": stringSchema(), "updated_at": stringSchema(),
		"goal":               object(map[string]any{"profile_id": stringSchema(), "storage_generation": stringSchema(), "effective_input_revision": stringSchema()}, "profile_id", "storage_generation", "effective_input_revision"),
		"integrity_verified": map[string]any{"type": "boolean"},
	}, "state", "reason", "updated_at", "integrity_verified")
}
func valueApplySchema() map[string]any {
	return object(map[string]any{
		"state":  map[string]any{"enum": []string{"requested", "applied", "failed", "paused"}},
		"reason": stringSchema(), "updated_at": stringSchema(), "execution_id": stringSchema(), "request_id": stringSchema(), "next_id": stringSchema(),
		"goal": object(map[string]any{"profile_id": stringSchema(), "storage_generation": stringSchema(), "effective_input_revision": stringSchema()}, "profile_id", "storage_generation", "effective_input_revision"),
	}, "state", "reason", "updated_at", "execution_id")
}
func inputObservationSchema() map[string]any {
	return object(map[string]any{"state": map[string]any{"enum": []string{"current", "pending", "failed", "unknown", "verification_pending"}}, "reason": stringSchema(), "delivered": inputVersionSchema(), "current": inputVersionSchema()}, "state", "reason", "delivered", "current")
}
func valueUseSchema() map[string]any {
	return object(map[string]any{"kind": map[string]any{"enum": []string{"declared", "delivered", "prepared"}}, "command": stringSchema(), "directory": stringSchema(), "env": stringSchema(), "selection": stringSchema(), "execution_id": stringSchema(), "execution_state": stringSchema(), "output_policy": outputPolicySchema(), "output": map[string]any{"type": "object"}, "values": inputObservationSchema()}, "kind", "command", "directory", "env", "selection", "values")
}
func (a *App) registerValueUsage() {
	options := append(profileOptions(true), Option{Name: "bundle", Pattern: project.ProfilePattern, Completion: completeSelection, Default: "all", Description: "Select all, none, or a shared bundle; no per-key arguments are required."})
	a.commands = append(a.commands, Command{Name: "values inspect", Description: "Inspect selected input identities without values, unlocking, or extending the key session.", Options: options, Output: object(map[string]any{"profile": stringSchema(), "env": stringSchema(), "selection": stringSchema(), "values": inputObservationSchema()}, "profile", "env", "selection", "values"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		store, e := a.store(r.Options)
		if e != nil {
			return nil, e
		}
		current, e := values.InspectInput(ctx, filepath.Dir(store.Directory), store.Profile, r.Options["env"], values.InputSpec{Inject: true, Selection: r.Options["bundle"]})
		observation := values.ObserveInput(nil, current, e)
		if e == nil {
			observation.Reason = "inspection_only"
		}
		return map[string]any{"profile": store.Profile, "env": r.Options["env"], "selection": r.Options["bundle"], "values": observation}, nil
	}})
	a.commands = append(a.commands, Command{Name: "values uses", Description: "Separate declared selections from recorded deliveries; this does not observe app reads.", Options: []Option{{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory."}}, Output: object(map[string]any{"profile": stringSchema(), "items": arraySchema(valueUseSchema())}, "profile", "items"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		p, e := project.Resolve(r.Options["dir"], "")
		if e != nil {
			return nil, e
		}
		store, e := a.serviceStore()
		if e != nil {
			return nil, e
		}
		items, e := store.ValueUses(ctx, p)
		if e != nil {
			return nil, e
		}
		return map[string]any{"profile": p.Profile, "items": items}, nil
	}})
}
