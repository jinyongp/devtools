package expose

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retainedbody"
)

// ReleasePending reports whether a closed exposure still owns registry resources.
func (r Record) ReleasePending(st *ports.State) bool {
	if r.Reservation != "" && st.Reservation(r.Reservation) != nil {
		return true
	}
	if st.Network != nil {
		for _, pin := range st.Network.Pins {
			if pin.ID == r.ID {
				return true
			}
		}
	}
	return false
}

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
	st, condition := (ports.Store{Directory: filepath.Join(s.Data, "ports")}).Read()
	if condition != nil {
		return condition
	}
	if r.ReleasePending(st) {
		return Failure("exposure_recovery_required")
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
