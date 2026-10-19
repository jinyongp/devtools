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

func TestAutomaticValueRestartRealCoordinatorOptInFailureLatchAndOverrides(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	app := testApp(t)
	initializeTestVault(t, app)
	root := privateTempDir(t)
	exe, _ := os.Executable()
	observed := filepath.Join(root, "observed.json")
	args, _ := json.Marshal([]string{exe, "__launcher-test-app", observed})
	once, _ := json.Marshal([]string{exe, "__launcher-test-once", filepath.Join(root, "once.json")})
	notifyArgs, _ := json.Marshal([]string{exe, "__launcher-test-app", filepath.Join(root, "notify.json")})
	readyArgs, _ := json.Marshal([]string{exe, "__launcher-test-once", filepath.Join(root, "ready.json")})
	config := filepath.Join(root, "devtools.toml")
	write := func(missing bool) {
		t.Helper()
		run := string(args)
		if missing {
			run = "[\"" + filepath.ToSlash(filepath.Join(root, "absent-executable")) + "\"]"
		}
		text := "profile='auto-fixture'\n[commands.web]\ninject=true\nenv='local'\nvalues_update='restart'\nexec=" + run + "\n[commands.once]\ninject=true\nvalues_update='restart'\nexec=" + string(once) + "\n[commands.notify]\ninject=true\nenv='local'\nexec=" + string(notifyArgs) + "\n[commands.web.ready]\nexec=" + string(readyArgs) + "\ntimeout='5s'\n"
		if err := os.WriteFile(config, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(false)
	if code, _, diagnostic := invoke(t, app, "", "env", "create", "local", "--profile", "auto-fixture"); code != 0 {
		t.Fatal(diagnostic)
	}
	set := func(env, value string) {
		t.Helper()
		argv := []string{"sec", "set", "TOKEN", "--profile", "auto-fixture", "--stdin"}
		if env != "" {
			argv = append(argv, "--env", env)
		}
		if code, _, diagnostic := invoke(t, app, value, argv...); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	set("", "synthetic-auto-common-one")
	set("local", "synthetic-auto-local-one")
	directory, e := app.dataDirectory()
	if e != nil {
		t.Fatal(e)
	}
	store := n05PreparedService(filepath.Dir(directory))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		items, _ := store.List(cleanup, "auto-fixture")
		for _, r := range items {
			if r.EndedAt == nil {
				store.Apply(cleanup, services.Request{Action: "stop", ID: r.ID, RequestID: tasks.ID()})
			}
		}
	})
	first, e := store.Apply(ctx, services.Request{Action: "start", Directory: root, Command: "web", RequestID: tasks.ID()})
	if e != nil {
		t.Fatal(e)
	}
	notify, e := store.Apply(ctx, services.Request{Action: "start", Directory: root, Command: "notify", RequestID: tasks.ID()})
	if e != nil {
		t.Fatal(e)
	}
	single, e := store.Apply(ctx, services.Request{Action: "start", Directory: root, Command: "once", RequestID: tasks.ID()})
	if e != nil {
		t.Fatal(e)
	}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.NewTimer(8 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for {
			if check() {
				return
			}
			select {
			case <-deadline.C:
				t.Fatal("automatic coordinator did not converge")
			case <-tick.C:
			}
		}
	}
	wait(func() bool { r, e := store.Status(ctx, single.Item.ID); return e == nil && r.EndedAt != nil })
	set("", "synthetic-auto-common-hidden")
	set("local", "synthetic-auto-local-one") // no-op
	time.Sleep(2300 * time.Millisecond)
	live, e := store.Status(ctx, first.Item.ID)
	if e != nil || live.EndedAt != nil || live.ValueApply != nil {
		t.Fatal("hidden common/no-op input caused replacement", e)
	}
	set("local", "synthetic-auto-local-two")
	var next services.Record
	wait(func() bool {
		r, e := store.Status(ctx, first.Item.ID)
		if e != nil || r.ValueApply == nil || r.ValueApply.State != "applied" {
			return false
		}
		next, e = store.Status(ctx, r.ValueApply.NextID)
		return e == nil && next.State == "running"
	})
	completed, e := store.Status(ctx, first.Item.ID)
	if e != nil || completed.ValueApply == nil || completed.ValueApply.Reason != "readiness_passed" {
		t.Fatal("automatic readiness success not distinguished", e)
	}
	if next.Previous != first.Item.ID || next.AutoOrigin != first.Item.ID || len(next.AutoRestarts) != 1 {
		t.Fatal("automatic replacement did not preserve chain")
	}
	body, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if json.Unmarshal(body, &got) != nil || got["token"] != "synthetic-auto-local-two" {
		t.Fatal("successor received wrong actual input")
	}
	// Retention of the ended original must not remove the current server's
	// readiness/failure guard. Its upstream completion lives with the successor.
	if err := os.Remove(filepath.Join(store.Data, "processes", first.Item.ID, "value-apply.json")); err != nil {
		t.Fatal(err)
	}
	// A preparation failure preserves the active successor and latches automation.
	write(true)
	set("local", "synthetic-auto-local-three")
	wait(func() bool {
		r, e := store.Status(ctx, next.ID)
		return e == nil && r.ValueApply != nil && r.ValueApply.State == "failed"
	})
	failed, e := store.Status(ctx, next.ID)
	if e != nil || failed.EndedAt != nil || failed.State != "running" {
		t.Fatal("pre-stop failure stopped old execution", e)
	}
	requestID := failed.ValueApply.RequestID
	set("local", "synthetic-auto-local-four")
	time.Sleep(2300 * time.Millisecond)
	failed, e = store.Status(ctx, next.ID)
	if e != nil || failed.ValueApply.RequestID != requestID || failed.EndedAt != nil {
		t.Fatal("new goal bypassed failure latch", e)
	}
	write(false)
	retried, e := store.Apply(ctx, services.Request{Action: "restart", ID: next.ID, RequestID: tasks.ID()})
	if e != nil || retried.Item.AutoOrigin != "" || len(retried.Item.AutoRestarts) != 0 {
		t.Fatal("explicit retry did not reset automation", e)
	}
	items, e := store.List(ctx, "auto-fixture")
	if e != nil || len(items) != 5 {
		t.Fatal("duplicate automatic or one-shot execution", len(items), e)
	}
	active := retried.Item
	for _, value := range []string{"synthetic-auto-burst-a", "synthetic-auto-burst-b", "synthetic-auto-burst-c"} {
		set("local", value)
		original := active
		wait(func() bool {
			r, e := store.Status(ctx, original.ID)
			if e != nil || r.ValueApply == nil || r.ValueApply.State != "applied" {
				return false
			}
			active, e = store.Status(ctx, r.ValueApply.NextID)
			return e == nil && active.State == "running"
		})
	}
	set("local", "synthetic-auto-burst-d")
	wait(func() bool {
		r, e := store.Status(ctx, active.ID)
		return e == nil && r.ValueApply != nil && r.ValueApply.State == "paused" && r.ValueApply.Reason == "restart_storm"
	})
	items, e = store.List(ctx, "auto-fixture")
	if e != nil || len(items) != 8 {
		t.Fatal("storm guard created another execution", len(items), e)
	}

	sibling, e := store.Status(ctx, notify.Item.ID)
	if e != nil || sibling.State != "running" || sibling.EndedAt != nil || sibling.ValueApply != nil {
		t.Fatal("default-notify sibling was replaced or stopped", e)
	}

}
