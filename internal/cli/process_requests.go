package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/protocol"
)

func savedRequestSchema(result map[string]any) map[string]any {
	return object(map[string]any{"item": object(map[string]any{
		"request_id": stringSchema(),
		"state":      map[string]any{"enum": []string{"pending", "completed"}},
		"result":     result,
	}, "request_id", "state")}, "item")
}

func (a *App) registerProcessRequests() {
	for _, group := range []string{"process", "project"} {
		result := lifecycleOutputSchema()
		if group == "process" {
			result = object(map[string]any{"item": managedProcessSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "item", "changed", "replayed")
		}
		a.commands = append(a.commands, Command{
			Name: group + " request show", Description: "Read a saved request without reading current project configuration or resuming work. Saved results describe the original operation; inspect execution status separately.",
			Arguments: []Argument{{Name: "request-id", Required: true, Pattern: uuidPattern}},
			Output:    savedRequestSchema(result),
			Run: func(_ context.Context, _ IO, r Request) (any, *protocol.Error) {
				if group == "process" {
					s, e := a.serviceStore()
					if e != nil {
						return nil, e
					}
					item, e := s.InspectRequest(r.Args[0])
					return map[string]any{"item": item}, e
				}
				m, e := a.projectLifecycleManager()
				if e != nil {
					return nil, e
				}
				item, e := m.InspectRequest(r.Args[0])
				return map[string]any{"item": item}, e
			},
		})
	}
}
