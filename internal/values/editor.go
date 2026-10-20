package values

import (
	"context"
	"sort"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// Editor exposes explicit mutations, not a plaintext State. It lives for one
// authenticated batch callback; only input-targeted slots may be opened.
type Editor struct {
	Profile     string
	state       *State
	source      *vault.Profile
	mutation    *vault.Mutation
	ctx         context.Context
	key, master *vault.Material
	vaultID     string
	promoted    bool
}

func newEditor(ctx context.Context, p *vault.Profile, m *vault.Mutation, key, master *vault.Material, vaultID string) *Editor {
	state, _ := metadataState(p)
	return &Editor{Profile: p.Name, state: state, source: p, mutation: m, ctx: ctx, key: key, master: master, vaultID: vaultID}
}
func (e *Editor) Set(kind Kind, name, env, value string) (bool, *protocol.Error) {
	if _, err := e.state.Set(kind, name, env, value); err != nil {
		return false, err
	}
	if kind == Secret && !e.source.Sealed && !e.promoted {
		if e.master == nil {
			return false, VaultError(e.ctx, vault.ErrKeyMissing)
		}
		key, err := e.mutation.Seal(e.vaultID, e.master)
		if err != nil {
			return false, VaultError(e.ctx, err)
		}
		e.key = key
		e.promoted = true
	}
	changed, err := e.mutation.Set(name, vault.ValueKind(kind), env, value)
	if err != nil {
		return false, VaultError(e.ctx, err)
	}
	return changed, nil
}
func (e *Editor) Unset(kind Kind, name, env string) (bool, *protocol.Error) {
	if _, err := e.state.Unset(kind, name, env); err != nil {
		return false, err
	}
	changed, err := e.mutation.Unset(name, vault.ValueKind(kind), env)
	if err != nil {
		return false, VaultError(e.ctx, err)
	}
	return changed, nil
}
func (e *Editor) CreateEnv(env string) (bool, *protocol.Error) {
	if _, err := e.state.CreateEnv(env); err != nil {
		return false, err
	}
	changed, err := e.mutation.CreateEnv(env)
	if err != nil {
		return false, VaultError(e.ctx, err)
	}
	return changed, nil
}
func (e *Editor) RemoveEnv(env string) (bool, *protocol.Error) {
	if _, err := e.state.RemoveEnv(env); err != nil {
		return false, err
	}
	changed, err := e.mutation.RemoveEnv(env)
	if err != nil {
		return false, VaultError(e.ctx, err)
	}
	return changed, nil
}
func (e *Editor) hydrate(input map[string]string, env string, skipInherited bool) *protocol.Error {
	for name := range input {
		if err := e.ctx.Err(); err != nil {
			return VaultError(e.ctx, err)
		}
		entry, ok := e.state.Keys[name]
		if !ok || entry.Kind != Secret {
			continue
		}
		if skipInherited {
			_, local := entry.Envs[env]
			if entry.Common != nil || local {
				continue
			}
		}
		value, exists, err := e.mutation.Value(name, env)
		if err != nil {
			return VaultError(e.ctx, err)
		}
		if exists {
			if env == "" {
				entry.Common = &value
			} else {
				entry.Envs[env] = value
			}
		}
	}
	return nil
}
func (e *Editor) PlanImport(input map[string]string, variables []string, env string, overwrite bool) ([]ImportItem, bool, *protocol.Error) {
	if err := e.state.CheckEnv(env); err != nil {
		return nil, false, err
	}
	if err := e.hydrate(input, env, false); err != nil {
		return nil, false, err
	}
	return e.state.PlanImport(input, variables, env, overwrite)
}
func (e *Editor) Import(input map[string]string, variables []string, env string, overwrite bool) ([]ImportItem, bool, *protocol.Error) {
	items, applicable, err := e.PlanImport(input, variables, env, overwrite)
	if err != nil {
		return nil, false, err
	}
	if !applicable {
		return nil, false, protocol.NewError("import_conflict", "Resolve kind conflicts and use --overwrite to replace existing values. Preview with --dry-run.", 3, map[string]any{"items": items})
	}
	changed := false
	for _, item := range items {
		if item.Action == "unchanged" {
			continue
		}
		updated, err := e.Set(item.Kind, item.Key, env, input[item.Key])
		if err != nil {
			return nil, false, err
		}
		changed = changed || updated
	}
	return items, changed, nil
}
func (e *Editor) batchImport(input map[string]string, variables []string, env string, overwrite, apply bool) ([]ImportItem, bool, *protocol.Error) {
	if err := e.state.CheckEnv(env); err != nil {
		return nil, false, err
	}
	if err := e.hydrate(input, env, !overwrite); err != nil {
		return nil, false, err
	}
	items, applicable, err := e.state.batchImport(input, variables, env, overwrite, false)
	if err != nil {
		return nil, false, err
	}
	if apply && !applicable {
		return items, false, protocol.NewError("import_conflict", "Keep the existing kinds for conflicting keys.", 3, nil)
	}
	if apply {
		for _, item := range items {
			if item.Action == "skip" || item.Action == "unchanged" {
				continue
			}
			if _, err := e.Set(item.Kind, item.Key, env, input[item.Key]); err != nil {
				return nil, false, err
			}
		}
	}
	return items, applicable, nil
}
func (e *Editor) bundle(c Change) (bool, error) {
	b := c.Bundle
	if b == nil || !vault.ValidBundleName(b.Name) || c.Import != nil || c.Key != "" || c.Value != nil || c.Env != "" {
		return false, vault.ErrInvalid
	}
	switch c.Action {
	case "bundle.set":
		if b.NewName != "" {
			return false, vault.ErrInvalid
		}
		return e.mutation.SetBundle(b.Name, b.Members)
	case "bundle.rename":
		if len(b.Members) != 0 {
			return false, vault.ErrInvalid
		}
		return e.mutation.RenameBundle(b.Name, b.NewName)
	case "bundle.remove":
		if len(b.Members) != 0 || b.NewName != "" {
			return false, vault.ErrInvalid
		}
		return e.mutation.RemoveBundle(b.Name)
	}
	return false, vault.ErrInvalid
}
func (e *Editor) importBundle(name string, items []ImportItem) (bool, error) {
	byID := map[string]string{}
	for _, v := range e.source.Values {
		byID[v.ID] = v.Name
	}
	selected := map[string]bool{}
	for _, b := range e.source.Bundles {
		if b.Name == name {
			for _, id := range b.ValueIDs {
				selected[byID[id]] = true
			}
		}
	}
	for _, item := range items {
		selected[item.Key] = true
	}
	members := make([]string, 0, len(selected))
	for name := range selected {
		members = append(members, name)
	}
	sort.Strings(members)
	return e.mutation.SetBundle(name, members)
}
