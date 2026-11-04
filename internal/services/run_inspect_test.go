package services

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestServiceControlRequiresLiveAuthenticationAndExactScope(t *testing.T) {
	for _, name := range []string{"controlled", "missing_id", "controller_lost", "authentication_failed", "profile", "directory", "instance", "command"} {
		t.Run(name, func(t *testing.T) {
			store := Store{Data: privateTempDir(t)}
			p := project.Context{Profile: "app", Root: privateTempDir(t)}
			now := time.Now().UTC()
			record := Record{ID: tasks.ID(), Profile: p.Profile, Directory: p.Root, Instance: "instance", Command: "web", Mode: "foreground", CreatedAt: now, StartedAt: &now, State: "running", Cleanup: initialCleanup(), Control: ControlStatus{State: "available", Stop: true}}
			switch name {
			case "profile":
				record.Profile = "other"
			case "directory":
				record.Directory = privateTempDir(t)
			case "instance":
				record.Instance = "other"
			case "command":
				record.Command = "other"
			}
			if err := store.RestoreRecord(record); err != nil {
				t.Fatal(err)
			}
			var hits atomic.Int32
			if name != "controller_lost" {
				token := strings.Repeat("a", 64)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					hits.Add(1)
					if name == "authentication_failed" || request.Header.Get("Authorization") != "Bearer "+token {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					_ = json.NewEncoder(w).Encode(record)
				}))
				defer server.Close()
				if err := writePrivate(store.path(record.ID, "control.json"), control{ID: record.ID, Address: server.URL, Token: token}); err != nil {
					t.Fatal(err)
				}
			}
			id := record.ID
			if name == "missing_id" {
				id = ""
			}
			port := 4400
			observed := ports.RunObservation{InstanceID: "instance", Service: "web", Port: &port, Claim: "active", Owner: map[string]any{"status": "verified", "kind": "foreground", "command": "web", "execution_id": id}, PortObservation: map[string]any{"status": "in_use"}, Listener: ports.ListenerObservation{Status: "observed", Processes: []ports.ListenerProcess{{PID: 42, Program: "node"}}}}
			resolved := store.resolveService(context.Background(), p, "web", observed)
			if name == "controlled" {
				if !resolved.Control.Stop || resolved.State != "controlled" || len(resolved.Remedies) != 1 || resolved.Remedies[0].Effect != "system_write" {
					t.Fatal(resolved)
				}
			} else if resolved.Control.Stop || len(resolved.Remedies) != 0 {
				t.Fatal("unverified or unrelated metadata authorized a stop", resolved)
			}
			if name == "missing_id" && (hits.Load() != 0 || resolved.Reason != "execution_not_registered") {
				t.Fatal("unregistered listener gained control", resolved, hits.Load())
			}
			if name == "profile" || name == "directory" || name == "instance" || name == "command" {
				if resolved.Reason != "execution_scope_mismatch" {
					t.Fatal("cross-scope controller was associated with this claim", resolved)
				}
			}
		})
	}
}

func TestForeignStrictPortRecoveryKeepsOwnerScopeAndReleasesRegistryBeforeControl(t *testing.T) {
	store := Store{Data: privateTempDir(t)}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	owner := project.Context{Profile: "owner", Root: privateTempDir(t), Ports: map[string]project.Port{"web": {Port: &port, Strict: true}}}
	command := project.Command{Exec: []string{"unused"}, Serve: []string{"web"}}
	id := tasks.ID()
	portStore := store.Ports()
	portStore.Probe = func(int) (bool, *protocol.Error) { return true, nil }
	portStore.Owner = ports.RunOwner{Kind: "foreground", Command: "web", ExecutionID: id}
	prepared, release, condition := portStore.Prepare(context.Background(), owner, command, "", nil, []int{port, port}, false)
	if condition != nil {
		t.Fatal(condition)
	}
	defer release()
	now := time.Now().UTC()
	record := Record{ID: id, Profile: owner.Profile, Directory: owner.Root, Instance: prepared.Assignments[0].ID, Command: "web", Mode: "foreground", CreatedAt: now, StartedAt: &now, State: "running", Cleanup: initialCleanup()}
	if err := store.RestoreRecord(record); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(record)
	}))
	defer server.Close()
	if err := writePrivate(store.path(id, "control.json"), control{ID: id, Address: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	selected := project.Context{Profile: "selected", Root: privateTempDir(t), Ports: owner.Ports, Commands: map[string]project.Command{"other": command}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	inspection, condition := store.InspectProject(ctx, selected, []string{"other"})
	if condition != nil || len(inspection.Observations) != 1 || len(inspection.NextActions) != 1 {
		t.Fatal(inspection, condition)
	}
	observed := inspection.Observations[0]
	if observed.State != "controlled" || observed.ExecutionID != id || observed.OwnerProfile != owner.Profile || observed.OwnerDirectory != owner.Root {
		t.Fatal("strict conflict lost the exact foreign owner", observed)
	}
	checked := false
	recoveryStore := store.Ports()
	inspect := recoveryStore.InspectFailure
	recoveryStore.InspectFailure = func(ctx context.Context, failure *protocol.Error) *protocol.Error {
		if issue := recoveryStore.Update(ctx, func(*ports.State) (bool, *protocol.Error) { return false, nil }); issue != nil {
			t.Fatal("registry was still locked during control inspection", issue)
		}
		checked = true
		return inspect(ctx, failure)
	}
	_, releaseFailed, failure := recoveryStore.Prepare(ctx, selected, command, "", nil, []int{port, port}, false)
	defer releaseFailed()
	if failure == nil || !checked {
		t.Fatal("strict conflict did not use common recovery", failure)
	}
	facts := failure.Details["facts"].(map[string]any)
	actions := failure.Details["remedies"].([]protocol.Remedy)
	if facts["owner_profile"] != owner.Profile || facts["owner_directory"] != owner.Root || len(actions) != 1 || actions[0].Effect != "system_write" {
		t.Fatal("recovery hid the owner or lost control", failure)
	}
}
