package expose

import (
	"context"

	"github.com/jinyongp/devtools/internal/protocol"
)

// Provider sessions retain the actual owned process or exact external handler.
// Persisted PIDs alone never authorize process termination.
type Provider interface {
	Start(context.Context, Record, string) (ProviderSession, *protocol.Error)
}
type ProviderSession interface {
	URL() string
	Owner() string
	Alive(context.Context) bool
	Stop(context.Context) *protocol.Error
}
