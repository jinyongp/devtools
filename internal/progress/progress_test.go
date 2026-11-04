package progress

import (
	"context"
	"testing"
)

func TestScopedProgressHasStableIdentityAndFinishesOnce(t *testing.T) {
	var events []Event
	ctx := WithObserver(context.Background(), func(event Event) { events = append(events, event) })
	parent := Begin(ctx, "Inspecting storage")
	child := Begin(ctx, "Waiting for storage access")
	parent()
	child()
	child()
	if len(events) != 4 || events[0].ID == events[1].ID || events[0].ID != events[2].ID || events[1].ID != events[3].ID || !events[2].Finished || !events[3].Finished {
		t.Fatal(events)
	}
	Begin(context.Background(), "No output consumer")()
	Suspend(context.Background())()
}

func TestSuspensionsHaveIndependentBalancedLifetimes(t *testing.T) {
	var events []Event
	ctx := WithObserver(context.Background(), func(event Event) { events = append(events, event) })
	first, second := Suspend(ctx), Suspend(ctx)
	first()
	first()
	second()
	if len(events) != 4 || events[0].ID == events[1].ID || events[0].ID != events[2].ID || events[1].ID != events[3].ID {
		t.Fatal(events)
	}
	for i, event := range events {
		if !event.Suspended || event.Finished != (i >= 2) || event.Message != "" {
			t.Fatal(events)
		}
	}
}
