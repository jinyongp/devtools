package expose

import (
	"context"
	"encoding/json"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retainedbody"
)

// Retire removes a resolved exposure from history while preserving the exact
// logical record needed by operation recovery. It does not delete a provider.
func (s Store) Retire(ctx context.Context, id string) *protocol.Error {
	r, condition := s.Read(id)
	if condition != nil {
		return condition
	}
	if r.State != "stopped" || r.EndedAt == nil {
		return Failure("exposure_active")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return Failure("invalid_storage")
	}
	if err := (retainedbody.Store{Root: s.Data}).WriteReference(ctx, s.Path(id), "archive_payload", body); err != nil {
		return Failure("io_error")
	}
	return nil
}
