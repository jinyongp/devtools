package valuehandoff

import (
	"context"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

var DeliveryStates = []string{"prepared", "approved", "delivering", "delivered", "unknown", "expired", "canceled"}
var ExecutionStates = []string{"not_started", "unknown", "reported_started", "reported_finished", "reported_failed"}

// AuthenticateQuery preserves protected-metadata access on snapshot reads.
// It only borrows an existing session; it never unlocks or extends authority.
func (s Store) AuthenticateQuery(ctx context.Context) *protocol.Error {
	a, err := s.access(ctx, false)
	if err != nil {
		return values.VaultError(ctx, err)
	}
	a.master.Destroy()
	return nil
}

func currentExecution(item Execution, latest map[string]Execution) bool {
	if item.ExecutionState == "reported_finished" {
		return false
	}
	if item.ExecutionState == "reported_failed" {
		next := latest[item.ExternalExecutionID]
		return next.ID == "" || next.ID == item.ID
	}
	if item.DeliveryState == "expired" || item.DeliveryState == "canceled" {
		return false
	}
	return true // Prepared, in-flight and genuinely unknown external outcomes.
}

func (s Store) Navigation(ctx context.Context, history bool) (ListResult, *protocol.Error) {
	out := ListResult{Profile: s.Profile, Items: []Execution{}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	records, _, e := s.listRecords(ctx)
	if e != nil {
		return out, e
	}
	latest := map[string]Execution{}
	observed := []Execution{}
	for _, r := range records {
		item := r.Item
		if r.RetiredAt == nil {
			var err error
			item, err = s.observed(ctx, r)
			if err != nil {
				return out, s.observationError(ctx, err, r.Item.ID)
			}
			observed = append(observed, item)
		}
		if item.ExecutionState != "reported_started" && item.ExecutionState != "reported_finished" && item.ExecutionState != "reported_failed" && item.DeliveryID == "" {
			continue
		}
		previous, ok := latest[item.ExternalExecutionID]
		if !ok || previous.CreatedAt.Before(item.CreatedAt) || previous.CreatedAt.Equal(item.CreatedAt) && previous.ID < item.ID {
			latest[item.ExternalExecutionID] = item
		}
	}
	for _, item := range observed {
		if history || currentExecution(item, latest) {
			out.Items = append(out.Items, item)
		}
	}
	return out, nil
}
