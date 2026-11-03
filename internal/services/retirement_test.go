package services

import (
	"context"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestRetirementUsesOwnLeaseAndProtectsActiveRestartChain(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	now := time.Now().UTC()
	ended := now.Add(-40 * 24 * time.Hour)
	old := Record{ID: tasks.ID(), Profile: "app-a", Directory: privateTempDir(t), Command: "old", Instance: "same-instance", State: "exited", CreatedAt: ended.Add(-time.Hour), EndedAt: &ended}
	live := Record{ID: tasks.ID(), Profile: old.Profile, Directory: old.Directory, Command: "web", Instance: old.Instance, State: "running", CreatedAt: now}
	if e := tasks.PrivateDir(s.path(live.ID, "")); e != nil {
		t.Fatal(e)
	}
	lease, held, e := s.claimLease(context.Background(), live.ID, true)
	if e != nil || !held {
		t.Fatal(held, e)
	}
	defer lease.unlockClose()
	blocked := s.RetirementBlocks(context.Background(), []Record{old, live})
	if blocked[old.ID] != "" || blocked[live.ID] == "" {
		t.Fatal(blocked)
	}
	live.Previous = old.ID
	blocked = s.RetirementBlocks(context.Background(), []Record{old, live})
	if blocked[old.ID] != "restart_chain" {
		t.Fatal(blocked)
	}
}
