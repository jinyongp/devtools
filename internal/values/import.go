package values

import (
	"sort"

	"github.com/jinyongp/devtools/internal/protocol"
)

type ImportItem struct {
	Key    string `json:"key"`
	Kind   Kind   `json:"kind"`
	Action string `json:"action"`
}

// PlanImport compares the selected layer, keeping existing profile-wide kinds.
func (s *State) PlanImport(input map[string]string, variables []string, env string, overwrite bool) ([]ImportItem, bool, *protocol.Error) {
	if err := s.CheckEnv(env); err != nil {
		return nil, false, err
	}
	public := map[string]bool{}
	for _, key := range variables {
		if _, ok := input[key]; !ok {
			return nil, false, protocol.NewError("invalid_argument", "Every --var key must exist in the input file.", 2, nil)
		}
		public[key] = true
	}
	keys := make([]string, 0, len(input))
	for key, value := range input {
		if !keyPattern.MatchString(key) || !validValue(value) {
			return nil, false, protocol.NewError("invalid_argument", "Use valid environment keys and UTF-8 values without NUL bytes.", 2, nil)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]ImportItem, 0, len(keys))
	applicable := true
	for _, key := range keys {
		kind := Secret
		if public[key] {
			kind = Variable
		}
		item := ImportItem{Key: key, Kind: kind, Action: "add"}
		if e, ok := s.Keys[key]; ok {
			item.Kind = e.Kind
			old, exists := e.Envs[env]
			if env == "" {
				exists = e.Common != nil
				if exists {
					old = *e.Common
				}
			}
			if public[key] && e.Kind == Secret {
				item.Action = "kind_conflict"
				applicable = false
			} else if exists {
				if old == input[key] {
					item.Action = "unchanged"
				} else if overwrite {
					item.Action = "update"
				} else {
					item.Action = "conflict"
					applicable = false
				}
			}
		}
		items = append(items, item)
	}
	return items, applicable, nil
}

func (s *State) Import(input map[string]string, variables []string, env string, overwrite bool) ([]ImportItem, bool, *protocol.Error) {
	items, applicable, err := s.PlanImport(input, variables, env, overwrite)
	if err != nil {
		return nil, false, err
	}
	if !applicable {
		return nil, false, protocol.NewError("import_conflict", "Resolve kind conflicts and use --overwrite to replace existing values. Preview with --dry-run.", 3, map[string]any{"items": items})
	}
	changed := false
	for _, item := range items {
		updated, err := s.Set(item.Kind, item.Key, env, input[item.Key])
		if err != nil {
			return nil, false, err
		}
		changed = changed || updated
	}
	return items, changed, nil
}
