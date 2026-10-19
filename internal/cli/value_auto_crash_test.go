package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestAutomaticCoordinatorRealDeathDuringOldStopDoesNotRepeat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app := testApp(t)
	initializeTestVault(t, app)
	root := privateTempDir(t)
	exe, _ := os.Executable()
	observed := filepath.Join(root, "observed.json")
	args, _ := json.Marshal([]string{exe, "__launcher-test-app", observed})
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='auto-crash'\n[commands.web]\ninject=true\nvalues_update='restart'\nexec="+string(args)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	set := func(value string) {
		t.Helper()
		if code, _, diagnostic := invoke(t, app, value, "sec", "set", "TOKEN", "--profile", "auto-crash", "--stdin"); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	set("synthetic-auto-crash-before")
	directory, e := app.dataDirectory()
	if e != nil {
		t.Fatal(e)
	}
	store := n05PreparedService(filepath.Dir(directory))
	first, e := store.Apply(ctx, services.Request{Action: "start", Directory: root, Command: "web", RequestID: tasks.ID()})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		os.WriteFile(filepath.Join(root, "release-stop"), []byte("release"), 0600)
		cleanup, stop := context.WithTimeout(context.Background(), 6*time.Second)
		defer stop()
		items, _ := store.List(cleanup, "auto-crash")
		for _, r := range items {
			if r.EndedAt == nil {
				store.Apply(cleanup, services.Request{Action: "stop", ID: r.ID, RequestID: tasks.ID()})
			}
		}
	})
	statusPath := filepath.Join(store.Data, "processes", first.Item.ID, "value-apply.json")
	writeStatus := func(status services.ValueApplyStatus) {
		t.Helper()
		body, _ := json.Marshal(status)
		if err := os.WriteFile(statusPath, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeStatus(services.ValueApplyStatus{State: "paused", ExecutionID: first.Item.ID})
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.NewTimer(6 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			if check() {
				return
			}
			select {
			case <-deadline.C:
				t.Fatal("owned coordinator barrier timed out")
			case <-tick.C:
			}
		}
	}
	wait(func() bool { _, e := os.Stat(observed); return e == nil })
	set("synthetic-auto-crash-after")
	goal := store.InspectRecordValues(ctx, first.Item).Current
	if goal == nil {
		t.Fatal("current goal absent")
	}
	request := services.ValueApplyStatus{State: "requested", Reason: "restart_requested", ExecutionID: first.Item.ID, RequestID: tasks.ID(), Goal: &goal.Identity, UpdatedAt: time.Now().UTC()}
	writeStatus(request)
	if err := os.WriteFile(filepath.Join(root, "pause-stop"), []byte("pause"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(request)
	child := exec.CommandContext(ctx, exe, "__values-restart", store.Data, string(body))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	})
	wait(func() bool { _, e := os.Stat(filepath.Join(root, "stop-entered")); return e == nil })
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("owned coordinator was not killed")
	}
	if err := os.WriteFile(filepath.Join(root, "release-stop"), []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { r, e := store.Status(ctx, first.Item.ID); return e == nil && r.EndedAt != nil })
	wait(func() bool {
		r, e := store.Status(ctx, request.RequestID)
		return e == nil && (r.EndedAt != nil || r.State == "interrupted")
	})
	// Advance the safe status timestamp to exercise the abandonment deadline
	// without sleeping ninety seconds. Actual process/launcher death is real.
	request.UpdatedAt = time.Now().Add(-91 * time.Second)
	writeStatus(request)
	ended, e := store.Status(ctx, first.Item.ID)
	if e != nil || ended.ValueApply == nil || ended.ValueApply.State != "paused" || ended.ValueApply.Reason != "coordinator_outcome_unknown" {
		t.Fatal("dead coordinator claimed completion", e)
	}
	for n := 0; n < 2; n++ {
		store.Apply(ctx, services.Request{Action: "restart", ID: first.Item.ID, RequestID: request.RequestID, AutoGoal: request.Goal})
	}
	items, e := store.List(ctx, "auto-crash")
	if e != nil || len(items) != 2 {
		t.Fatal("coordinator death duplicated execution", len(items), e)
	}
	for _, r := range items {
		if r.State == "running" {
			t.Fatal("canceled frozen launcher started after coordinator death")
		}
	}
}
