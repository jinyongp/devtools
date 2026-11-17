package tasks

import (
	"context"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s *State) resolveReference(ctx context.Context, kind, input string) (string, *protocol.Error) {
	return idref.Resolve(ctx, idref.UUID, input, func(yield func(string) bool) {
		if kind == "run" {
			for id := range s.Runs {
				if !yield(id) {
					return
				}
			}
			return
		}
		for id, item := range s.Items {
			if item.Kind == kind && !yield(id) {
				return
			}
		}
	})
}
