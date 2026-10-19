package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestLostPreparedRestartReconcilesConfirmedOutage(t *testing.T) {
	for _, tc := range []struct {
		name, phase    string
		oldEnded, want bool
	}{{"old still active", "admitted", false, false}, {"old stop completed", "admitted", true, true}, {"commit proves completed stop", "committed", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := Store{Data: privateTempDir(t)}
			now := time.Now().UTC()
			old := Record{ID: tasks.ID(), Profile: "app", Directory: privateTempDir(t), Command: "web", CreatedAt: now, StartedAt: &now, State: "running"}
			if tc.oldEnded {
				old.EndedAt = &now
				old.State = "stopped"
			}
			if e := writePrivate(store.path(old.ID, "record.json"), old); e != nil {
				t.Fatal(e)
			}
			candidate := Record{ID: tasks.ID(), Profile: "app", Directory: old.Directory, Command: "web", CreatedAt: now, State: "starting", Previous: old.ID, InputSnapshotID: strings.Repeat("a", 32), LaunchPhase: tc.phase}
			lease, e := store.prepareLaunch(candidate)
			if e != nil {
				t.Fatal(e)
			}
			if err := lease.unlockClose(); err != nil {
				t.Fatal(err)
			}
			recovered, changed, e := store.terminalizeLost(ctx, candidate, true)
			if e != nil || !changed || recovered.EndedAt == nil || recovered.StartedAt != nil || recovered.Outage != tc.want {
				t.Fatal("incorrect lost launch recovery", recovered.Outage, e)
			}
		})
	}
}

func TestEndedPreparedRestartReportsLateStopWithoutRewritingRecord(t *testing.T) {
	ctx := context.Background()
	store := Store{Data: privateTempDir(t)}
	now := time.Now().UTC()
	old := Record{ID: tasks.ID(), Profile: "app", Directory: privateTempDir(t), Command: "web", CreatedAt: now, StartedAt: &now, State: "running"}
	candidate := Record{ID: tasks.ID(), Profile: "app", Directory: old.Directory, Command: "web", CreatedAt: now, EndedAt: &now, State: "interrupted", Previous: old.ID, InputSnapshotID: strings.Repeat("b", 32), LaunchPhase: "admitted"}
	for _, r := range []Record{old, candidate} {
		if e := writePrivate(store.path(r.ID, "record.json"), r); e != nil {
			t.Fatal(e)
		}
	}
	initial, e := store.Status(ctx, candidate.ID)
	if e != nil || initial.Outage {
		t.Fatal("unconfirmed old stop guessed", e)
	}
	old.EndedAt = &now
	old.State = "stopped"
	if e := writePrivate(store.path(old.ID, "record.json"), old); e != nil {
		t.Fatal(e)
	}
	late, e := store.Status(ctx, candidate.ID)
	if e != nil || !late.Outage {
		t.Fatal("late stop missing in passive status", e)
	}
	stored, e := store.read(candidate.ID)
	if e != nil || stored.Outage {
		t.Fatal("passive status rewrote historical record", e)
	}
	resumed, e := store.start(ctx, Request{RequestID: candidate.ID}, old.ID)
	if e != nil || !resumed.Item.Outage || resumed.Item.StartedAt != nil {
		t.Fatal("same execution retry lost confirmed outage", e)
	}
}
