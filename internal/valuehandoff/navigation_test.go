package valuehandoff

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestExecutionNavigationKeepsUnresolvedAndSeparatesResolvedHistory(t *testing.T) {
	s, client := fixture(t)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		prepared, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: fmt.Sprintf("job-%d", i), Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(5000+i))
		if e != nil {
			t.Fatal(e)
		}
		a, err := s.access(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		gate, err := s.lockCommit(ctx, a)
		if err != nil {
			a.master.Destroy()
			t.Fatal(err)
		}
		r, err := s.readRecord(ctx, a, prepared.Item.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0:
			r.Item.ExpiresAt = time.Now().Add(-time.Hour)
		case 1:
			r.Item.DeliveryState = "canceled"
		case 2:
			r.Item.DeliveryState, r.Item.ExecutionState = "delivered", "reported_finished"
		case 3:
			r.Item.DeliveryState, r.Item.ExecutionState = "unknown", "unknown"
		case 4:
			r.Item.DeliveryState, r.Item.ExecutionState = "delivered", "reported_failed"
		}
		path := s.path("value-executions", r.Item.ID)
		body, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, r)
		if err == nil {
			err = maintenance.Replace(s.Root, map[string][]byte{path: body})
		}
		gate()
		a.master.Destroy()
		if err != nil {
			t.Fatal(err)
		}
	}
	current, e := s.Navigation(ctx, false)
	if e != nil || len(current.Items) != 3 {
		t.Fatal(current, e)
	}
	for _, item := range current.Items {
		if item.DeliveryState == "expired" || item.DeliveryState == "canceled" || item.ExecutionState == "reported_finished" {
			t.Fatal(item)
		}
	}
	history, e := s.Navigation(ctx, true)
	if e != nil || len(history.Items) != 6 {
		t.Fatal(history, e)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if e := s.AuthenticateQuery(ctx); e == nil || e.Code != "vault_locked" {
		t.Fatal("cached protected metadata bypassed session", e)
	}
}
