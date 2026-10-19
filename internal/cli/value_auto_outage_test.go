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

func TestAutomaticRestartPostStopSpawnAndReadinessFailuresRemainExplicit(t *testing.T) {
	for _, stage := range []string{"spawn", "readiness"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			app := testApp(t)
			initializeTestVault(t, app)
			root := privateTempDir(t)
			exe, _ := os.Executable()
			observed := filepath.Join(root, "observed.json")
			args, _ := json.Marshal([]string{exe, "__launcher-test-app", observed})
			config := filepath.Join(root, "devtools.toml")
			write := func(argv, ready string) {
				t.Helper()
				if err := os.WriteFile(config, []byte("profile='auto-outage'\n[commands.web]\ninject=true\nvalues_update='restart'\nexec="+argv+"\n"+ready), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(string(args), "")
			set := func(value string) {
				t.Helper()
				if code, _, diagnostic := invoke(t, app, value, "sec", "set", "TOKEN", "--profile", "auto-outage", "--stdin"); code != 0 {
					t.Fatal(diagnostic)
				}
			}
			set("synthetic-auto-outage-before")
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
				items, _ := store.List(cleanup, "auto-outage")
				for _, r := range items {
					if r.EndedAt == nil {
						store.Apply(cleanup, services.Request{Action: "stop", ID: r.ID, RequestID: tasks.ID()})
					}
				}
			})
			wait := func(check func() bool) {
				t.Helper()
				deadline := time.NewTimer(8 * time.Second)
				defer deadline.Stop()
				tick := time.NewTicker(20 * time.Millisecond)
				defer tick.Stop()
				for {
					if check() {
						return
					}
					select {
					case <-deadline.C:
						t.Fatal("automatic failure did not converge", stage)
					case <-tick.C:
					}
				}
			}
			wait(func() bool { _, err := os.Stat(observed); return err == nil })
			if err := os.WriteFile(filepath.Join(root, "pause-stop"), []byte("pause"), 0600); err != nil {
				t.Fatal(err)
			}
			candidateExe := exe
			if stage == "spawn" {
				candidateExe = filepath.Join(root, "next.exe")
				b, err := os.ReadFile(exe)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(candidateExe, b, 0700); err != nil {
					t.Fatal(err)
				}
			}
			candidateArgs, _ := json.Marshal([]string{candidateExe, "__launcher-test-once", filepath.Join(root, "candidate.json")})
			readyArgs, _ := json.Marshal([]string{exe, "__redaction-test-once"})
			ready := ""
			if stage == "readiness" {
				ready = "[commands.web.ready]\nexec=" + string(readyArgs) + "\ntimeout='1s'\n"
			}
			write(string(candidateArgs), ready)
			set("synthetic-auto-outage-after")
			wait(func() bool { _, err := os.Stat(filepath.Join(root, "stop-entered")); return err == nil })
			if stage == "spawn" {
				if err := os.Remove(candidateExe); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "release-stop"), []byte("release"), 0600); err != nil {
				t.Fatal(err)
			}
			var outcome *services.ValueApplyStatus
			wait(func() bool {
				r, e := store.Status(ctx, first.Item.ID)
				if e == nil && r.ValueApply != nil && r.ValueApply.State == "failed" {
					outcome = r.ValueApply
					return true
				}
				return false
			})
			if outcome.NextID == "" {
				t.Fatal("post-stop failure omitted candidate identity")
			}
			next, e := store.Status(ctx, outcome.NextID)
			if e != nil || next.Previous != first.Item.ID {
				t.Fatal("candidate identity changed", e)
			}
			if stage == "spawn" && (!next.Outage || next.StartedAt != nil) {
				t.Fatal("spawn failure did not report outage")
			}
			if stage == "readiness" && next.StartedAt == nil {
				t.Fatal("readiness case never launched")
			}
			set("synthetic-auto-outage-third")
			time.Sleep(2200 * time.Millisecond)
			items, e := store.List(ctx, "auto-outage")
			if e != nil || len(items) != 2 {
				t.Fatal("failed automatic operation repeated", len(items), e)
			}
		})
	}
}
