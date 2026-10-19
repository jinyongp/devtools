package values

import (
	"context"
	"reflect"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

const (
	InputChangeDebounce    = 500 * time.Millisecond
	InputChangeMaxWait     = 5 * time.Second
	InputReconcileInterval = 2 * time.Second
	inputHintPoll          = 250 * time.Millisecond
)

// reconcileSchedule merges hints, but time alone also schedules authentication.
// Hint revision ordering is deliberately not an authority or a progress cursor.
type reconcileSchedule struct {
	checked           bool
	hintSeen          bool
	hintPresent       bool
	hintFailed        bool
	hint              vault.CommitHint
	dirty             bool
	first, last, next time.Time
}

func (s *reconcileSchedule) observe(hint *vault.CommitHint, failed bool, now time.Time) {
	present := hint != nil
	changed := !s.hintSeen || present != s.hintPresent || failed != s.hintFailed || present && *hint != s.hint
	s.hintSeen = true
	s.hintPresent = present
	s.hintFailed = failed
	if present {
		s.hint = *hint
	}
	if !changed {
		return
	}
	if !s.dirty {
		s.first = now
		s.dirty = true
	}
	s.last = now
}
func (s *reconcileSchedule) due(now time.Time) bool {
	if !s.checked {
		return true
	}
	if s.dirty {
		return !now.Before(s.last.Add(InputChangeDebounce)) || !now.Before(s.first.Add(InputChangeMaxWait))
	}
	return !now.Before(s.next)
}
func (s *reconcileSchedule) reconciled(now time.Time) {
	s.checked = true
	s.dirty = false
	s.next = now.Add(InputReconcileInterval)
}

func cloneObservation(v InputObservation) InputObservation {
	v.Delivered = v.Delivered.Clone()
	v.Current = v.Current.Clone()
	return v
}
func publishLatest(ch chan InputObservation, v InputObservation) {
	copy := cloneObservation(v)
	select {
	case ch <- copy:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	ch <- copy // one producer, with at most one pending metadata goal
}

// WatchInputChanges retains only safe input metadata. Dropped/duplicated/stale
// hints and helper reconnection do not suppress periodic authenticated reads.
// The consumer receives the latest observation rather than an unbounded backlog.
func WatchInputChanges(ctx context.Context, hint func(context.Context) (*vault.CommitHint, error), inspect func(context.Context) InputObservation) <-chan InputObservation {
	updates := make(chan InputObservation, 1)
	go func() {
		defer close(updates)
		ticker := time.NewTicker(inputHintPoll)
		defer ticker.Stop()
		schedule := reconcileSchedule{}
		var previous *InputObservation
		for {
			if ctx.Err() != nil {
				return
			}
			now := time.Now()
			h, e := hint(ctx)
			schedule.observe(h, e != nil, now)
			if schedule.due(now) {
				observation := inspect(ctx)
				schedule.reconciled(time.Now())
				if ctx.Err() != nil {
					return
				}
				if previous == nil || !reflect.DeepEqual(*previous, observation) {
					copied := cloneObservation(observation)
					previous = &copied
					publishLatest(updates, observation)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return updates
}
