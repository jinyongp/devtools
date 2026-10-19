package services

import (
	"os"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestValueApplyFailureOutcomeAndStormWindow(t *testing.T) {
	now := time.Now().UTC()
	history := []time.Time{now.Add(-AutoRestartWindow - time.Nanosecond), now.Add(-AutoRestartWindow), now.Add(-time.Second), now}
	recent := recentAutoRestarts(history, now)
	if len(recent) != 2 || !recent[0].Equal(history[2]) {
		t.Fatal("storm window boundary")
	}
	history[2] = time.Time{}
	if recent[0].IsZero() {
		t.Fatal("history retained caller slice")
	}
	store := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	goal := vault.InputIdentity{ProfileID: tasks.ID(), StorageGeneration: tasks.ID(), Revision: "opaque-metadata-goal"}
	request := ValueApplyStatus{State: "requested", Reason: "restart_requested", ExecutionID: id, RequestID: tasks.ID(), Goal: &goal, UpdatedAt: now}
	if err := writePrivate(store.path(id, "value-apply.json"), request); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteValueApply(request, tasks.ID(), "readiness_timeout"); err != nil {
		t.Fatal(err)
	}
	failed, e := store.ReadValueApply(id)
	if e != nil || failed.State != "failed" || failed.Reason != "readiness_timeout" {
		t.Fatal("failure not retained", e)
	}
	if err := store.CompleteValueApply(request, tasks.ID(), "values_delivered"); err != nil {
		t.Fatal(err)
	}
	stillFailed, _ := store.ReadValueApply(id)
	if stillFailed.State != "failed" || stillFailed.NextID != failed.NextID {
		t.Fatal("late reply overwrote terminal failure")
	}
	request.UpdatedAt = time.Now().UTC()
	if err := writePrivate(store.path(id, "value-apply.json"), request); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteValueApply(request, "", "launch_outcome_unknown"); err != nil {
		t.Fatal(err)
	}
	unknown, e := store.ReadValueApply(id)
	if e != nil || unknown.State != "paused" || unknown.Reason != "launch_outcome_unknown" {
		t.Fatal("unknown launch assumed failed/canceled", e)
	}
	request.UpdatedAt = now.Add(-91 * time.Second)
	if err := writePrivate(store.path(id, "value-apply.json"), request); err != nil {
		t.Fatal(err)
	}
	lost, e := store.ReadValueApply(id)
	if e != nil || lost.State != "paused" || lost.Reason != "coordinator_outcome_unknown" {
		t.Fatal("abandoned operation assumed success", e)
	}
	if err := os.WriteFile(store.path(id, "value-apply.json"), make([]byte, 8193), 0600); err != nil {
		t.Fatal(err)
	}
	if _, e := store.ReadValueApply(id); e == nil {
		t.Fatal("unbounded status accepted")
	}
}
