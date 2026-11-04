package completion

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestObserveKeepsHealthyCandidatesBesideSlowController(t *testing.T) {
	started := time.Now()
	items := Observe(context.Background(), []string{"slow", "ready", "failed"}, func(ctx context.Context, value string) (string, bool) {
		switch value {
		case "slow":
			<-ctx.Done()
			return "", false
		case "failed":
			return "", false
		}
		return value, true
	})
	if !reflect.DeepEqual(items, []string{"ready"}) || time.Since(started) > time.Second {
		t.Fatal(items, time.Since(started))
	}
}

func TestObserveDoesNotStartAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := Observe(ctx, []string{"candidate"}, func(context.Context, string) (string, bool) {
		t.Error("canceled observation started")
		return "", false
	})
	if len(items) != 0 {
		t.Fatal(items)
	}
}
