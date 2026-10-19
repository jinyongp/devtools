package services

import (
	"context"
	"time"

	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

// ValueWatchStatus is a compact live observation. Full delivery references stay
// in InputVersion; no values, tickets or plaintext-derived hashes are retained.
type ValueWatchStatus struct {
	State             string               `json:"state"`
	Reason            string               `json:"reason"`
	UpdatedAt         time.Time            `json:"updated_at"`
	Goal              *vault.InputIdentity `json:"goal,omitempty"`
	IntegrityVerified bool                 `json:"integrity_verified"`
}

func (s Store) startValueWatch(ctx context.Context, read func() Record, publish func(ValueWatchStatus)) func() {
	if read().InputVersion == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	updates := values.WatchInputChanges(ctx, func(context.Context) (*vault.CommitHint, error) { return vault.ReadCommitHint(s.Data) }, func(ctx context.Context) values.InputObservation {
		r := read()
		observation := s.InspectRecordValues(ctx, r)
		s.considerValueRestart(ctx, r, observation)
		return observation
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for observation := range updates {
			if ctx.Err() != nil {
				return
			}
			status := ValueWatchStatus{State: observation.State, Reason: observation.Reason, UpdatedAt: time.Now()}
			if observation.Current != nil {
				goal := observation.Current.Identity
				status.Goal = &goal
				status.IntegrityVerified = observation.Current.IntegrityVerified
			}
			publish(status)
		}
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}
