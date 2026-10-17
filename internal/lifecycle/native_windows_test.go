package lifecycle_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/cli"
	"github.com/jinyongp/devtools/internal/lifecycle"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

// Test entrypoints exercise the actual exported supervisor/preparation path.
// Public CLI compatibility gates stay closed until W7C.
func TestMain(m *testing.M) {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "__process-serve":
			if len(os.Args) != 5 {
				os.Exit(90)
			}
			lease, err := services.OpenInheritedLease(os.Args[4])
			if err != nil {
				os.Exit(91)
			}
			if err = cli.ServeProcessWithLease(context.Background(), os.Args[2], os.Args[3], lease); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(92)
			}
			os.Exit(0)
		case "__w4d-project-app":
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt)
			fmt.Println("project child started")
			<-signals
			os.Exit(0)
		case "__w4d-project-probe":
			if len(os.Args) == 3 {
				if _, err := os.Stat(os.Args[2]); err != nil {
					os.Exit(7)
				}
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestWindowsProjectRealSupervisorsRetryRestartDownAndReadiness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	data := filepath.Join(t.TempDir(), "data")
	root := t.TempDir()
	exe, _ := os.Executable()
	flag := filepath.Join(root, "ready.flag")
	argv := func(args ...string) string { b, _ := json.Marshal(append([]string{exe}, args...)); return string(b) }
	// One selected probe remains unready until an explicit fixture flag appears.
	config := "profile=\"native-project\"\n" +
		"[commands.web]\nexec=" + argv("__w4d-project-app") + "\n[commands.web.ready]\nexec=" + argv("__w4d-project-probe") + "\n" +
		"[commands.peer]\nexec=" + argv("__w4d-project-app") + "\n[commands.peer.ready]\nexec=" + argv("__w4d-project-probe") + "\n" +
		"[commands.delayed]\nexec=" + argv("__w4d-project-app") + "\n[commands.delayed.ready]\nexec=" + argv("__w4d-project-probe", flag) + "\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	p, failure := project.Resolve(root, "")
	if failure != nil {
		t.Fatal(failure)
	}
	store := services.Store{Data: data}
	manager := lifecycle.Manager{Data: data, Processes: store}
	capture := true
	t.Cleanup(func() {
		manager.Apply(context.Background(), lifecycle.Request{Action: lifecycle.ActionDown, Project: p, RequestID: tasks.ID()})
	})
	up := lifecycle.Request{Action: lifecycle.ActionUp, Project: p, Commands: []string{"web", "peer"}, Timeout: 10 * time.Second, Capture: &capture, RequestID: tasks.ID()}
	first, failure := manager.Apply(ctx, up)
	if failure != nil || len(first.Items) != 2 || first.Items[0].Status != "ready" || first.Items[1].Status != "ready" {
		t.Fatalf("up: %+v %v", first, failure)
	}
	web, peer := first.Items[0].Item.ID, first.Items[1].Item.ID
	replay, failure := manager.Apply(ctx, up)
	if failure != nil || !replay.Replayed || replay.Items[0].Item.ID != web || replay.Items[1].Item.ID != peer {
		t.Fatalf("up replay: %+v %v", replay, failure)
	}
	restarted, failure := manager.Apply(ctx, lifecycle.Request{Action: lifecycle.ActionRestart, Project: p, Commands: []string{"web"}, Timeout: 10 * time.Second, RequestID: tasks.ID()})
	if failure != nil || len(restarted.Items) != 1 || restarted.Items[0].Item.ID == web || restarted.Items[0].Item.Previous != web {
		t.Fatalf("selected restart: %+v %v", restarted, failure)
	}
	alive, failure := store.Status(ctx, peer)
	if failure != nil || alive.State != "running" {
		t.Fatalf("restart touched peer: %+v %v", alive, failure)
	}
	down := lifecycle.Request{Action: lifecycle.ActionDown, Project: p, Commands: []string{"web"}, RequestID: tasks.ID()}
	stopped, failure := manager.Apply(ctx, down)
	if failure != nil || len(stopped.Items) != 1 {
		t.Fatalf("down: %+v %v", stopped, failure)
	}
	if replay, failure := manager.Apply(ctx, down); failure != nil || !replay.Replayed {
		t.Fatalf("down replay: %+v %v", replay, failure)
	}
	alive, failure = store.Status(ctx, peer)
	if failure != nil || alive.State != "running" {
		t.Fatalf("down touched peer: %+v %v", alive, failure)
	}
	delayed := lifecycle.Request{Action: lifecycle.ActionUp, Project: p, Commands: []string{"peer", "delayed"}, Timeout: 2 * time.Second, RequestID: tasks.ID()}
	partial, failure := manager.Apply(ctx, delayed)
	if failure == nil || len(partial.Items) != 2 || partial.Items[0].Item.ID != peer || partial.Items[1].Item == nil {
		t.Fatalf("partial readiness: %+v %v", partial, failure)
	}
	delayedID := partial.Items[1].Item.ID
	alive, failure = store.Status(ctx, peer)
	if failure != nil || alive.State != "running" {
		t.Fatalf("partial failure touched peer: %+v %v", alive, failure)
	}
	if err := os.WriteFile(flag, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	converged, failure := manager.Apply(ctx, delayed)
	if failure != nil || !converged.Replayed || converged.Items[0].Item.ID != peer || converged.Items[1].Item.ID != delayedID || converged.Items[1].Status != "ready" {
		t.Fatalf("partial retry: %+v %v", converged, failure)
	}
}
