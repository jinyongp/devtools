package proxy

import (
	"context"

	"github.com/jinyongp/devtools/internal/expose"
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
	for _, r := range records {
		if !history && r.State == "stopped" || selected != nil && !selected(r.Public) {
			continue
		}
		item := r.Public
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
