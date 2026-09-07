package values

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"sort"
)

type BatchImport struct {
	Content   string   `json:"content"`
	Variables []string `json:"variables"`
	Overwrite bool     `json:"overwrite"`
	Preview   bool     `json:"preview"`
}

// Dashboard imports can retain effective values, including inherited common values.
func (s *State) batchImport(input map[string]string, variables []string, env string, overwrite, apply bool) ([]ImportItem, bool, *protocol.Error) {
	if len(input) == 0 {
		return nil, false, protocol.NewError("invalid_argument", "Provide at least one .env assignment.", 2, nil)
	}
	if len(input) > 5000 {
		return nil, false, protocol.NewError("invalid_argument", "Import at most 5,000 keys at a time.", 2, nil)
	}
	items, applicable, err := s.PlanImport(input, variables, env, true)
	if err != nil {
		return nil, false, err
	}
	if !overwrite {
		for n := range items {
			i := &items[n]
			if entry, ok := s.Keys[i.Key]; ok {
				_, local := entry.Envs[env]
				if entry.Common != nil || local {
					i.Action = "skip"
				}
			}
		}
		applicable = true
		for _, i := range items {
			if i.Action == "kind_conflict" {
				applicable = false
			}
		}
	}
	if apply && !applicable {
		return items, false, protocol.NewError("import_conflict", "Keep the existing kinds for conflicting keys.", 3, nil)
	}
	if apply {
		for _, i := range items {
			if i.Action == "skip" || i.Action == "unchanged" {
				continue
			}
			if _, err = s.Set(i.Kind, i.Key, env, input[i.Key]); err != nil {
				return nil, false, err
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, applicable, nil
}
