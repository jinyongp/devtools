package values

import (
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestInputWatchDebounceMaxWaitAndPeriodicFallback(t *testing.T) {
	start := time.Unix(100, 0)
	schedule := reconcileSchedule{}
	schedule.observe(nil, false, start)
	if !schedule.due(start) {
		t.Fatal("startup did not authenticate current storage")
	}
	schedule.reconciled(start)
	if schedule.due(start.Add(InputReconcileInterval-time.Nanosecond)) || !schedule.due(start.Add(InputReconcileInterval)) {
		t.Fatal("lost hints suppressed periodic reconciliation")
	}
	first := start.Add(100 * time.Millisecond)
	hint := vault.CommitHint{Revision: 1}
	schedule.observe(&hint, false, first)
	schedule.observe(&hint, false, first.Add(400*time.Millisecond)) // duplicate cannot postpone
	if schedule.due(first.Add(InputChangeDebounce-time.Nanosecond)) || !schedule.due(first.Add(InputChangeDebounce)) {
		t.Fatal("quiet debounce deadline changed")
	}
	schedule.reconciled(first.Add(InputChangeDebounce))
	first = first.Add(time.Second)
	for n := range 50 {
		hint.Revision = uint64(n + 2)
		schedule.observe(&hint, false, first.Add(time.Duration(n)*100*time.Millisecond))
	}
	if schedule.due(first.Add(InputChangeMaxWait-time.Nanosecond)) || !schedule.due(first.Add(InputChangeMaxWait)) {
		t.Fatal("continuous changes exceeded maximum merge window")
	}
	schedule.reconciled(first.Add(InputChangeMaxWait))
	hint.Revision = 1 // delayed old event still asks for current storage, never rolls back
	schedule.observe(&hint, false, first.Add(6*time.Second))
	if !schedule.due(first.Add(6*time.Second + InputChangeDebounce)) {
		t.Fatal("out-of-order hint was treated as an authenticated cursor")
	}
}

func TestInputWatchSlowConsumerKeepsOnlyLatestOwnedMetadata(t *testing.T) {
	updates := make(chan InputObservation, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for n := range 1000 {
			publishLatest(updates, InputObservation{State: "pending", Current: &InputVersion{KeyEpoch: uint64(n), Slots: []vault.SlotReference{{Name: "A", Version: "original"}}}})
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow consumer blocked reconciliation")
	}
	if len(updates) != 1 {
		t.Fatal("unbounded metadata backlog")
	}
	got := <-updates
	if got.Current.KeyEpoch != 999 {
		t.Fatal("consumer did not receive latest goal")
	}
	input := InputObservation{Current: &InputVersion{Slots: []vault.SlotReference{{Name: "A", Version: "original"}}}}
	publishLatest(updates, input)
	input.Current.Slots[0].Version = "mutated"
	if (<-updates).Current.Slots[0].Version != "original" {
		t.Fatal("queued metadata aliases producer")
	}
}
