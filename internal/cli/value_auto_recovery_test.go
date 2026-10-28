package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestAutomaticCoordinatorRecoversHistoryBeforeConfiguration(t *testing.T) {
	for _, outcome := range []string{"proxy_failure", "stopped_success"} {
		t.Run(outcome, func(t *testing.T) {
			data, root := privateTempDir(t), privateTempDir(t)
			ctx := context.Background()
			now := time.Now().UTC()
			old, next, requestID := tasks.ID(), tasks.ID(), tasks.ID()
			goal := &vault.InputIdentity{ProfileID: tasks.ID(), StorageGeneration: tasks.ID(), Revision: "revision"}
			request := services.ValueApplyStatus{State: "requested", Reason: "restart_requested", UpdatedAt: now, ExecutionID: old, RequestID: requestID, Goal: goal}
			q := services.Request{Action: "restart", ID: old, RequestID: requestID, AutoGoal: goal}
			r := services.Record{ID: next, Profile: "app", Directory: root, Command: "web", CreatedAt: now, StartedAt: &now, State: "running", Previous: old}
			write := func(path string, v any) {
				t.Helper()
				if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
					t.Fatal(e)
				}
				if e := tasks.WritePrivate(path, v); e != nil {
					t.Fatal(e)
				}
			}
			if outcome == "proxy_failure" {
				input := struct {
					Profile, Root string
					Request       services.Request
				}{"app", root, q}
				_, e := (netops.Store{Data: data}).ChooseExecution(ctx, requestID, input, network.PrepareAuto, func() (netops.ExecutionPlan, *protocol.Error) {
					return netops.ExecutionPlan{Commands: []string{"web"}}, nil
				}, root)
				if e != nil {
					t.Fatal(e)
				}
				snapshot := &network.ExecutionSnapshot{ID: tasks.ID()}
				q.ProxyMode = true
				q.Network = snapshot
				r.ProxyMode = true
				r.NetworkSnapshotID = snapshot.ID
				r.StartedAt = nil
				r.EndedAt = &now
				r.State = "failed"
				r.Reason = "execution_failed"
				r.Outage = true
				write(filepath.Join(data, "processes", next, "network.json"), snapshot)
			}
			body, _ := json.Marshal(q)
			digest := sha256.Sum256(body)
			write(filepath.Join(data, "processes", "receipts", requestID+".json"), map[string]any{"fingerprint": hex.EncodeToString(digest[:]), "done": true, "result": services.Result{Item: r, Changed: true}})
			if outcome == "stopped_success" {
				r.State = "stopped"
				r.EndedAt = &now
			}
			write(filepath.Join(data, "processes", next, "record.json"), r)
			write(filepath.Join(data, "processes", old, "value-apply.json"), request)
			if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("[broken\n"), 0600); e != nil {
				t.Fatal(e)
			}
			encoded, _ := json.Marshal(request)
			if e := ServeValueRestart(ctx, data, string(encoded)); e != nil {
				t.Fatal(e)
			}
			result, e := (services.Store{Data: data}).ReadValueApply(old)
			want := "execution_failed"
			if outcome == "stopped_success" {
				want = "process_not_running"
			}
			if e != nil || result.State != "failed" || result.Reason != want || result.NextID != next {
				t.Fatal(result, e)
			}
		})
	}
}
