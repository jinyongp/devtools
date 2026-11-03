package dashboard

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Contracts struct {
	ResolveAction protocol.ActionResolver
	Collection    func(context.Context, string, string, string, map[string][]string) (map[string]any, *protocol.Error)
}
