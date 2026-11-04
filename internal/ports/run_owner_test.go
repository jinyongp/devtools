package ports

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestRunOwnerMetadataRejectsLegacyAndReusedPID(t *testing.T) {
	f, err := os.OpenFile(filepath.Join(privateTestRoot(t), "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	owner := func() map[string]any {
		return activeRunError(f).Details["facts"].(map[string]any)["owner"].(map[string]any)
	}
	if got := owner(); got["status"] != "unavailable" || got["reason"] != "legacy_run" {
		t.Fatal(got)
	}
	publishRunOwner(f, RunOwner{Kind: "foreground", Command: "dev:web"})
	if got := owner(); got["status"] != "verified" || got["pid"] != os.Getpid() || got["command"] != "dev:web" {
		t.Fatal(got)
	}
	_, _ = f.Seek(1, io.SeekStart)
	var metadata runMetadata
	if json.NewDecoder(f).Decode(&metadata) != nil {
		t.Fatal("metadata missing")
	}
	metadata.Identity = "previous-process-with-the-same-PID"
	if err := f.Truncate(1); err != nil {
		t.Fatal(err)
	}
	_, _ = f.Seek(1, io.SeekStart)
	if err := json.NewEncoder(f).Encode(metadata); err != nil {
		t.Fatal(err)
	}
	if got := owner(); got["status"] != "unavailable" || got["reason"] != "state_changed" || got["pid"] != nil {
		t.Fatal(got)
	}
	metadata.PID = 2147483647
	if err := f.Truncate(1); err != nil {
		t.Fatal(err)
	}
	_, _ = f.Seek(1, io.SeekStart)
	if err := json.NewEncoder(f).Encode(metadata); err != nil {
		t.Fatal(err)
	}
	if got := owner(); got["reason"] != "process_unavailable" || got["pid"] != nil || got["execution_id"] != nil {
		t.Fatal("process inspection failure was mislabeled as a changed run", got)
	}
	denied := activeRunError(ownerMetadataReadDenied{f}).Details["facts"].(map[string]any)["owner"].(map[string]any)
	if denied["reason"] != "permission_denied" || denied["pid"] != nil || denied["execution_id"] != nil {
		t.Fatal("metadata permission failure lost its cause", denied)
	}
	clearRunOwner(f)
	if got := owner(); got["reason"] != "legacy_run" {
		t.Fatal(got)
	}
	for _, malformed := range []RunOwner{{Kind: "foreground", Command: "web", ExecutionID: "REQUEST_UUID"}, {Kind: "foreground", Command: "web\nother"}} {
		publishRunOwner(f, malformed)
		if got := owner(); got["reason"] != "metadata_unavailable" || got["execution_id"] != nil || got["command"] != nil {
			t.Fatal("invalid metadata escaped into executable recovery or the public schema", got)
		}
	}
}

type ownerMetadataReadDenied struct{ *os.File }

func (ownerMetadataReadDenied) Seek(int64, int) (int64, error) { return 0, os.ErrPermission }

func TestRunFailureKeepsAssignmentAndUnknownOwner(t *testing.T) {
	e := describeRunFailure(protocol.NewError("port_run_active", "Original.", 3, nil), "app", "/project", "instance", "web", 3000)
	facts := e.Details["facts"].(map[string]any)
	if facts["port"] != 3000 || facts["service"] != "web" || facts["owner"].(map[string]any)["status"] != "unavailable" {
		t.Fatal(facts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observed := observeRunFailure(ctx, e)
	if observed != e || observed.Code != "port_run_active" || facts["port_observation"].(map[string]any)["status"] != "unavailable" {
		t.Fatal(observed)
	}
}

func TestPersistedOwnerDoesNotAuthorizeAStop(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, kind := range []string{"managed", "foreground"} {
		e := describeRunFailure(protocol.NewError("port_run_active", "Original.", 3, map[string]any{"facts": map[string]any{"owner": map[string]any{"status": "verified", "kind": kind, "execution_id": id}}}), "app", "/work/app", "instance", "web", 3000)
		action := e.Details["remedies"].([]protocol.Remedy)[0]
		if action.Effect != "read" || len(action.RequiredInputs) != 0 || !reflect.DeepEqual(action.Argv, []string{"devtools", "process", "status", id}) {
			t.Fatal(kind, action)
		}
	}
}

func TestRunFailureWithoutControlOffersScopedInspection(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		owner      map[string]any
	}{
		{"unregistered", "port_run_active", map[string]any{"status": "verified", "kind": "foreground", "command": "dev:web", "pid": os.Getpid()}},
		{"legacy", "port_run_active", unavailableOwner("legacy_run")},
		{"permission", "port_run_active", unavailableOwner("permission_denied")},
		{"changed", "port_run_active", unavailableOwner("state_changed")},
		{"metadata", "port_run_active", unavailableOwner("metadata_unavailable")},
		{"identity", "port_run_active", unavailableOwner("process_unavailable")},
		{"port", "port_in_use", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := describeRunFailure(protocol.NewError(tc.code, "Original.", 3, map[string]any{"facts": map[string]any{"owner": tc.owner}}), "app", "/work/app", "instance", "web", 3000)
			action := e.Details["remedies"].([]protocol.Remedy)[0]
			if action.Effect != "read" || len(action.RequiredInputs) != 0 || len(action.Argv) < 3 || !reflect.DeepEqual(action.Argv[:3], []string{"devtools", "project", "status"}) {
				t.Fatal(action)
			}
			argv := strings.Join(action.Argv, " ")
			for _, option := range []string{"--profile=app", "--dir=/work/app"} {
				if !strings.Contains(argv, option) {
					t.Fatal("inspection lost its scope or execution IDs", action)
				}
			}
			for _, forbidden := range []string{"process stop", "process list", "--view", "--state", "Ctrl+C", "original terminal", "Identify its controller", "REQUEST_UUID"} {
				if strings.Contains(argv+action.Message, forbidden) {
					t.Fatal("unverified stop guidance", action)
				}
			}
		})
	}
}

func TestGlobalRunConflictInspectionKeepsItsOwnScope(t *testing.T) {
	e := describeRunFailure(protocol.NewError("port_run_active", "Original.", 3, map[string]any{"facts": map[string]any{"owner": unavailableOwner("permission_denied")}}), "", "", "global-route", "api", 3000)
	action := e.Details["remedies"].([]protocol.Remedy)[0]
	if action.Effect != "read" || !reflect.DeepEqual(action.Argv, []string{"devtools", "proxy", "env", "api", "--global=true"}) || e.Details["facts"].(map[string]any)["owner"].(map[string]any)["reason"] != "permission_denied" {
		t.Fatal("global conflict lost its route or observation failure", action)
	}
}

func TestStrictInitialAllocationRetainsServiceConflictContext(t *testing.T) {
	store := testPortStore(t)
	store.Probe = func(int) (bool, *protocol.Error) { return false, nil }
	port := 32123
	p := project.Context{Profile: "app", Root: privateTestRoot(t), Ports: map[string]project.Port{"web": {Port: &port, Strict: true}}}
	_, release, condition := store.Prepare(context.Background(), p, project.Command{Exec: []string{"unused"}, Serve: []string{"web"}}, "", nil, []int{32100, 32199}, false)
	defer release()
	if condition == nil || condition.Code != "port_in_use" {
		t.Fatal(condition)
	}
	facts := condition.Details["facts"].(map[string]any)
	if facts["port"] != port || facts["service"] != "web" || condition.Details["target"].(map[string]any)["directory"] != p.Root {
		t.Fatal("initial allocation dropped the recovery target", condition.Details)
	}
}
