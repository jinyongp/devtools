package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func n05PreparedService(data string) services.Store {
	s := services.Store{Data: data}
	s.PrepareInput = func(ctx context.Context, r services.Record, n *network.ExecutionSnapshot) (execution.Prepared, *protocol.Error) {
		return services.PrepareFrozenInput(ctx, r, n, func(command execution.Command) (execution.Dependencies, *protocol.Error) {
			return execution.DependenciesFor(command, data, "")
		})
	}
	return s
}

func runN05RestartClient(data, oldID, requestID, stage string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	service := n05PreparedService(data)
	prepare := service.PrepareInput
	service.PrepareInput = func(ctx context.Context, r services.Record, n *network.ExecutionSnapshot) (execution.Prepared, *protocol.Error) {
		prepared, e := prepare(ctx, r, n)
		if e == nil && stage == "prepared" {
			if err := os.WriteFile(filepath.Join(r.Directory, "client-prepared"), []byte("ready"), 0600); err != nil {
				prepared.Release()
				return execution.Prepared{}, protocol.NewError("io_error", "Owned barrier failed.", 1, nil)
			}
			<-ctx.Done()
			prepared.Release()
			return execution.Prepared{}, protocol.NewError("canceled", "Owned barrier canceled.", 130, nil)
		}
		return prepared, e
	}
	if _, e := service.Apply(ctx, services.Request{Action: "restart", ID: oldID, RequestID: requestID}); e != nil {
		return 97
	}
	return 0
}

func TestRestartRealClientCrashBeforeAdmissionAndDuringOldStop(t *testing.T) {
	for _, stage := range []string{"prepared", "old-stop"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			app := testApp(t)
			initializeTestVault(t, app)
			root := privateTempDir(t)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			observed := filepath.Join(root, "observed.json")
			arguments, _ := json.Marshal([]string{exe, "__launcher-test-app", observed})
			if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='client-crash'\n[commands.web]\ninject=true\nexec="+string(arguments)+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if code, _, diagnostic := invoke(t, app, "synthetic-client-crash-canary", "sec", "set", "TOKEN", "--profile", "client-crash", "--stdin"); code != 0 {
				t.Fatal(diagnostic)
			}
			directory, e := app.dataDirectory()
			if e != nil {
				t.Fatal(e)
			}
			service := n05PreparedService(filepath.Dir(directory))
			t.Cleanup(func() {
				os.WriteFile(filepath.Join(root, "release-stop"), []byte("release"), 0600)
				cleanup, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				items, _ := service.List(cleanup, "client-crash")
				for _, r := range items {
					if r.EndedAt == nil {
						service.Apply(cleanup, services.Request{Action: "stop", ID: r.ID, RequestID: tasks.ID()})
					}
				}
			})
			first, e := service.Apply(ctx, services.Request{Action: "start", Command: "web", Directory: root, RequestID: tasks.ID()})
			if e != nil || first.Item.State != "running" {
				diagnostic, _ := json.Marshal(e)
				t.Fatalf("old execution did not start: error=%s state=%s phase=%s", diagnostic, first.Item.State, first.Item.LaunchPhase)
			}
			wait := func(check func() bool) {
				t.Helper()
				deadline := time.NewTimer(5 * time.Second)
				defer deadline.Stop()
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for {
					if check() {
						return
					}
					select {
					case <-deadline.C:
						t.Fatal("owned process barrier timed out")
					case <-tick.C:
					}
				}
			}
			wait(func() bool { _, err := os.Stat(observed); return err == nil })
			if err := os.Remove(observed); err != nil {
				t.Fatal(err)
			}
			if stage == "old-stop" {
				if err := os.WriteFile(filepath.Join(root, "pause-stop"), []byte("pause"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			request := services.Request{Action: "restart", ID: first.Item.ID, RequestID: tasks.ID()}
			child := exec.CommandContext(ctx, exe, "__n05-restart-client", service.Data, first.Item.ID, request.RequestID, stage)
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if child.ProcessState == nil {
					child.Process.Kill()
					child.Wait()
				}
			})
			marker := "client-prepared"
			if stage == "old-stop" {
				marker = "stop-entered"
			}
			wait(func() bool { _, err := os.Stat(filepath.Join(root, marker)); return err == nil })
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("owned client was not killed")
			}
			if stage == "prepared" {
				old, e := service.Status(ctx, first.Item.ID)
				if e != nil || old.State != "running" {
					t.Fatal("client crash before admission stopped old app", e)
				}
			} else {
				if err := os.WriteFile(filepath.Join(root, "release-stop"), []byte("release"), 0600); err != nil {
					t.Fatal(err)
				}
				wait(func() bool { old, e := service.Status(ctx, first.Item.ID); return e == nil && old.EndedAt != nil })
				wait(func() bool {
					candidate, e := service.Status(ctx, request.RequestID)
					return e == nil && (candidate.EndedAt != nil || candidate.State == "interrupted")
				})
			}
			validOutcome := func(result services.Result, e *protocol.Error) bool {
				if stage != "old-stop" {
					return e == nil
				}
				if e == nil {
					return false
				}
				// A live supervisor can confirm cancellation after the client
				// closes its channel. A lost supervisor only proves uncertainty.
				// Scheduling and native process lifetime determine which is seen.
				switch result.Item.Reason {
				case "launch_canceled":
					return result.Item.State == "failed" && e.Code == "execution_failed" && e.ExitCode == 126
				case "stop_requested":
					return result.Item.State == "stopped" && e.Code == "process_not_running"
				case "supervisor_lost":
					return result.Item.State == "interrupted" && e.Code == "launch_outcome_unknown"
				case "launch_outcome_unknown":
					return e.Code == "launch_outcome_unknown"
				}
				return false
			}
			result, e := service.Apply(ctx, request)
			if !validOutcome(result, e) || !result.Replayed || result.Item.ID != request.RequestID {
				diagnostic, _ := json.Marshal(e)
				t.Fatalf("crash retry did not reuse original execution: error=%s state=%s phase=%s replayed=%v same_id=%v", diagnostic, result.Item.State, result.Item.LaunchPhase, result.Replayed, result.Item.ID == request.RequestID)
			}
			if stage == "old-stop" {
				if result.Item.StartedAt != nil || !result.Item.Outage {
					t.Fatalf("post-stop client death lost non-spawn/outage: state=%s outage=%v", result.Item.State, result.Item.Outage)
				}
				if _, err := os.Stat(observed); !os.IsNotExist(err) {
					t.Fatal("canceled launcher spawned after client death")
				}
			} else if result.Item.StartedAt == nil {
				t.Fatal("safe pre-admission retry did not start")
			}
			for range 3 {
				replay, e := service.Apply(ctx, request)
				if !validOutcome(replay, e) || !replay.Replayed || replay.Item.ID != result.Item.ID || stage == "old-stop" && (replay.Item.StartedAt != nil || !replay.Item.Outage) {
					t.Fatal("same UUID duplicated crash retry", e)
				}
			}
			records, e := service.List(ctx, "client-crash")
			if e != nil || len(records) != 2 {
				t.Fatal("unexpected duplicate execution record", e)
			}
		})
	}
}
