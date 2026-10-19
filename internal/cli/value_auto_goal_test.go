package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestAutomaticGoalGuardRejectsChangedInputBeforeStoppingOldExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app := testApp(t)
	initializeTestVault(t, app)
	root := privateTempDir(t)
	exe, _ := os.Executable()
	args, _ := json.Marshal([]string{exe, "__launcher-test-app", filepath.Join(root, "observed.json")})
	config := filepath.Join(root, "devtools.toml")
	write := func(policy string) {
		t.Helper()
		if err := os.WriteFile(config, []byte("profile='goal-guard'\n[commands.web]\ninject=true\nvalues_update='"+policy+"'\nexec="+string(args)+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("restart")
	set := func(value string) {
		t.Helper()
		if code, _, diagnostic := invoke(t, app, value, "sec", "set", "TOKEN", "--profile", "goal-guard", "--stdin"); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	set("synthetic-goal-one")
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
		cleanup, stop := context.WithTimeout(context.Background(), 6*time.Second)
		defer stop()
		items, _ := store.List(cleanup, "goal-guard")
		for _, r := range items {
			if r.EndedAt == nil {
				store.Apply(cleanup, services.Request{Action: "stop", ID: r.ID, RequestID: tasks.ID()})
			}
		}
	})
	original := first.Item
	goal := original.InputVersion.Identity
	set("synthetic-goal-two")
	request := services.Request{Action: "restart", ID: original.ID, RequestID: tasks.ID(), AutoGoal: &goal}
	if _, e := store.Apply(ctx, request); e == nil || e.Code != "value_goal_changed" {
		t.Fatal("stale automatic goal accepted", e)
	}
	live, e := store.Status(ctx, original.ID)
	if e != nil || live.State != "running" || live.EndedAt != nil {
		t.Fatal("stale goal stopped original", e)
	}
	view := store.InspectRecordValues(ctx, live)
	if view.Current == nil {
		t.Fatal("current goal absent")
	}
	current := view.Current.Identity
	write("notify")
	if _, e := store.Apply(ctx, services.Request{Action: "restart", ID: original.ID, RequestID: tasks.ID(), AutoGoal: &current}); e == nil || e.Code != "values_update_disabled" {
		t.Fatal("revoked opt-in ignored", e)
	}
	write("restart")
	accepted := services.Request{Action: "restart", ID: original.ID, RequestID: tasks.ID(), AutoGoal: &current}
	next, e := store.Apply(ctx, accepted)
	if e != nil || next.Item.Previous != original.ID || next.Item.ID != accepted.RequestID {
		t.Fatal("valid automatic goal failed", e)
	}
	// Inject loss of the completion receipt, retaining the actual committed
	// successor and request fingerprint. Retry must reconcile that same child.
	receiptPath := filepath.Join(store.Data, "processes", "receipts", accepted.RequestID+".json")
	b, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if json.Unmarshal(b, &receipt) != nil {
		t.Fatal("receipt fixture")
	}
	receipt["Done"] = false
	// Encoding/json names untagged receipt fields with their Go field names.
	b, _ = json.Marshal(receipt)
	if err := os.WriteFile(receiptPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	retry, e := store.Apply(ctx, accepted)
	if e != nil || retry.Item.ID != next.Item.ID || !retry.Replayed {
		t.Fatal("pending automatic request did not reconcile exact successor", e)
	}
	if _, e := store.Apply(ctx, services.Request{Action: "stop", ID: original.ID, RequestID: tasks.ID()}); e != nil {
		t.Fatal(e)
	}
	if _, e := store.Apply(ctx, services.Request{Action: "restart", ID: original.ID, RequestID: tasks.ID(), AutoGoal: &current}); e == nil || e.Code != "process_not_running" {
		t.Fatal("stale automatic task resurrected stopped server", e)
	}
}
