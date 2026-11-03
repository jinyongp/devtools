package cli

import "github.com/jinyongp/devtools/internal/protocol"

func (a *App) listPortScope(r Request, instances bool) (any, *protocol.Error) {
	store, e := a.portStore()
	if e != nil {
		return nil, e
	}
	state, e := store.Read()
	if e != nil {
		return nil, e
	}
	items := []map[string]any{}
	if instances {
		for _, instance := range state.Instances {
			row := instanceResult(instance)
			if r.Scope.matches(row) {
				items = append(items, row)
			}
		}
	} else {
		for _, assignment := range state.Assignments {
			row := assignmentResult(assignment)
			if r.Scope.matches(row) {
				items = append(items, row)
			}
		}
	}
	return map[string]any{"items": items}, nil
}
