package proxy

import (
	"context"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
)

// ExposureNavigation filters public scope/provider metadata before contacting
// any active provider. Resolved stopped attempts belong to explicit history.
func (m Manager) ExposureNavigation(ctx context.Context, history bool, selected func(expose.Public) bool) ([]expose.Public, *protocol.Error) {
	records, e := (expose.Store{Data: m.Data}).List()
	if e != nil {
		return nil, e
	}
	out := []expose.Public{}
	var state *ports.State
	for _, r := range records {
		if selected != nil && !selected(r.Public) {
			continue
		}
		item := r.Public
		if r.State == "stopped" {
			if state == nil {
				state, e = m.portStore().Read()
				if e != nil {
					return nil, e
				}
			}
			var current bool
			item, current = exposureCurrent(r, state)
			if !history && !current {
				continue
			}
		}
		if r.State == "active" || r.State == "starting" {
			item, e = m.ExposureStatus(ctx, r.ID)
			if e != nil {
				return nil, e
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func exposureCurrent(r expose.Record, st *ports.State) (expose.Public, bool) {
	if r.State != "stopped" {
		return r.Public, true
	}
	pending := r.ReleasePending(st)
	if pending {
		r.State, r.Reason = "recovery_required", "release_pending"
	}
	return r.Public, pending
}
