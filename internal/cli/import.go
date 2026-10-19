package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func (a *App) registerImport() {
	options := append(profileOptions(true),
		Option{Name: "bundle", Pattern: project.ProfilePattern, Description: "Add all imported shared ValueIDs to this reusable var/sec bundle atomically."},
		Option{Name: "file", Required: true, MinLength: 1, Description: "Read dotenv assignments from a regular UTF-8 file."},
		Option{Name: "var", Repeatable: true, Pattern: values.KeyPattern, Description: "Classify a new key as variable; repeat for each public key. Existing kinds are preserved."},
		Option{Name: "overwrite", Boolean: true, Description: "Replace different values in the selected scope."},
		Option{Name: "dry-run", Boolean: true, Description: "Preview classification and conflicts with no storage changes or value output."},
	)
	item := object(map[string]any{"key": stringSchema(), "kind": map[string]any{"enum": []string{"variable", "secret"}}, "action": map[string]any{"enum": []string{"add", "update", "unchanged", "conflict", "kind_conflict"}}}, "key", "kind", "action")
	a.commands = append(a.commands, Command{Name: "import", Description: "Import dotenv values atomically; new keys default to secret. Return metadata only.", Options: options,
		Output: object(map[string]any{"profile": stringSchema(), "env": stringSchema(), "dry_run": map[string]any{"type": "boolean"}, "changed": map[string]any{"type": "boolean"}, "applicable": map[string]any{"type": "boolean"}, "items": map[string]any{"type": "array", "items": item}, "warnings": warningsSchema(), "bundle": stringSchema()}, "profile", "env", "dry_run", "changed", "applicable", "items"), Run: a.importCommand})
}

func (a *App) importCommand(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	store, err := a.store(request.Options)
	if err != nil {
		return nil, err
	}
	env := request.Options["env"]
	state, err := store.Read(ctx)
	if err != nil {
		return nil, err
	}
	if err := state.CheckEnv(env); err != nil {
		return nil, err
	}
	input, err := readSecret(ctx, streams, map[string]string{"file": request.Options["file"]})
	if err != nil {
		return nil, err
	}
	parsed, err := values.ParseDotenv(input)
	if err != nil {
		return nil, err
	}
	dryRun := request.Options["dry-run"] == "true"
	overwrite := request.Options["overwrite"] == "true"
	var items []values.ImportItem
	changed, applicable := false, true
	if bundle := request.Options["bundle"]; bundle != "" {
		view, e := store.Inspect(ctx, env)
		if e != nil {
			return nil, e
		}
		result, e := store.Apply(ctx, values.Change{Action: "import", Env: env, Revision: view.Revision, RequestID: tasks.ID(), Import: &values.BatchImport{LayerOnly: true, Content: input, Variables: request.ListOptions["var"], Overwrite: overwrite, Preview: dryRun, Bundle: bundle}})
		if e != nil {
			return nil, e
		}
		items, changed, applicable = result.Items, result.Changed, result.Applicable
	} else if dryRun {
		items, applicable, err = store.PlanImport(ctx, parsed, request.ListOptions["var"], env, overwrite)
	} else {
		changed, err = store.Update(ctx, func(current *values.State) (bool, *protocol.Error) {
			var updated bool
			var failure *protocol.Error
			items, updated, failure = current.Import(parsed, request.ListOptions["var"], env, overwrite)
			return updated, failure
		})
	}
	if err != nil {
		return nil, err
	}
	result := map[string]any{"profile": store.Profile, "env": env, "dry_run": dryRun, "changed": changed, "applicable": applicable, "items": items}
	if bundle := request.Options["bundle"]; bundle != "" {
		result["bundle"] = bundle
	}
	if warnings := values.ImportWarnings(items); len(warnings) > 0 {
		result["warnings"] = warnings
	}
	return result, nil
}
