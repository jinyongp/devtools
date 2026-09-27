package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestStaleSupervisorAndRetryConflict(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	r := Record{ID: id, Profile: "test", Directory: t.TempDir(), State: "running", CreatedAt: time.Now()}
	if e := writePrivate(s.path(id, "record.json"), r); e != nil {
		t.Fatal(e)
	}
	status, e := s.Status(context.Background(), id)
	if e != nil || status.State != "interrupted" {
		t.Fatal(status, e)
	}
	req := Request{Action: "stop", ID: id, RequestID: tasks.ID()}
	result, e := s.Apply(context.Background(), req)
	if e != nil || result.Item.State != "interrupted" {
		t.Fatal(result, e)
	}
	result, e = s.Apply(context.Background(), req)
	if e != nil || !result.Replayed {
		t.Fatal(result, e)
	}
	req.Action = "restart"
	if _, e = s.Apply(context.Background(), req); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
	if _, e = s.Status(context.Background(), "../../outside"); e == nil || e.Code != "invalid_argument" || e.ExitCode != 2 {
		t.Fatal("invalid ID contract", e)
	}
	if _, e = s.Status(context.Background(), tasks.ID()); e == nil || e.Code != "process_not_found" || e.ExitCode != 3 {
		t.Fatal("missing execution contract", e)
	}
}
func TestIncompleteReceiptRetryPreservesChangedAndReportsReplay(t *testing.T) {
	s := Store{Data: t.TempDir()}
	requestID := tasks.ID()
	directory := t.TempDir()
	started := time.Now().UTC()
	record := Record{ID: requestID, Profile: "app", Instance: "instance", Directory: directory, Command: "web", CreatedAt: started, StartedAt: &started, State: "running"}
	if err := writePrivate(s.path(requestID, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	request := Request{Action: "start", Directory: directory, Command: "web", RequestID: requestID}
	body, _ := json.Marshal(request)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(body))
	receiptPath := filepath.Join(s.root(), "receipts", requestID+".json")
	if err := writePrivate(receiptPath, receipt{Fingerprint: fingerprint}); err != nil {
		t.Fatal(err)
	}

	result, err := s.Apply(context.Background(), request)
	if err != nil || !result.Changed || !result.Replayed || result.Item.ID != requestID {
		t.Fatalf("incomplete retry lost mutation metadata: %#v %v", result, err)
	}
	replayed, err := s.Apply(context.Background(), request)
	if err != nil || !replayed.Changed || !replayed.Replayed || replayed.Item.ID != requestID {
		t.Fatalf("completed replay drifted: %#v %v", replayed, err)
	}
}

func TestIncompleteStartReceiptStaysPendingUntilExecutionStarts(t *testing.T) {
	s := Store{Data: t.TempDir()}
	requestID := tasks.ID()
	directory := t.TempDir()
	record := Record{ID: requestID, Profile: "app", Instance: "instance", Directory: directory, Command: "web", CreatedAt: time.Now().UTC(), State: "starting"}
	if err := writePrivate(s.path(requestID, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	request := Request{Action: "start", Directory: directory, Command: "web", RequestID: requestID}
	body, _ := json.Marshal(request)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(body))
	receiptPath := filepath.Join(s.root(), "receipts", requestID+".json")
	if err := writePrivate(receiptPath, receipt{Fingerprint: fingerprint, Result: Result{Item: record, Changed: true}}); err != nil {
		t.Fatal(err)
	}

	pending, err := s.Apply(context.Background(), request)
	if err == nil || err.Code != "process_pending" || !pending.Changed || !pending.Replayed || pending.Item.ID != requestID || pending.Item.State != "starting" {
		t.Fatalf("starting execution was prematurely completed: %#v %v", pending, err)
	}
	var stored receipt
	if err := tasks.ReadPrivate(receiptPath, &stored); err != nil || stored.Done {
		t.Fatalf("pending receipt was marked done: %#v %v", stored, err)
	}

	started := time.Now().UTC()
	record.StartedAt = &started
	record.State = "running"
	if err := writePrivate(s.path(requestID, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	completed, err := s.Apply(context.Background(), request)
	if err != nil || !completed.Changed || !completed.Replayed || completed.Item.State != "running" || completed.Item.StartedAt == nil {
		t.Fatalf("pending retry did not converge to running: %#v %v", completed, err)
	}
}

func TestStopPendingReturnsAcceptedMutation(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	started := time.Now().UTC()
	record := Record{ID: id, Profile: "app", Instance: "instance", Directory: t.TempDir(), Command: "web", CreatedAt: started, StartedAt: &started, State: "running"}
	if err := writePrivate(s.path(id, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/stop" || request.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(record)
	}))
	defer server.Close()
	if err := writePrivate(s.path(id, "control.json"), control{ID: id, Address: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	result, err := s.stop(ctx, record)
	if err == nil || err.Code != "process_pending" || !result.Changed || result.Item.ID != id {
		t.Fatalf("pending stop lost accepted mutation: %#v %v", result, err)
	}
}

func TestStartRunsFallbackPreflightBeforeCreatingExecution(t *testing.T) {
	s := Store{Data: t.TempDir()}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='app'\n[commands.web]\nexec=['/bin/true']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	requestID := tasks.ID()
	called := 0
	request := Request{Action: "start", Directory: root, Command: "web", RequestID: requestID, BeforeStart: func(context.Context) *protocol.Error {
		called++
		return protocol.NewError("requirements_failed", "fallback preflight failed", 3, nil)
	}}
	result, err := s.start(context.Background(), request, "")
	if err == nil || err.Code != "requirements_failed" || called != 1 || result.Item.ID != "" || result.Changed {
		t.Fatalf("fallback preflight did not stop execution creation: result=%#v called=%d err=%v", result, called, err)
	}
	if _, err := s.read(requestID); err == nil || err.Code != "process_not_found" {
		t.Fatalf("execution record was created before fallback preflight: %v", err)
	}
	state, portErr := (ports.Store{Directory: filepath.Join(s.Data, "ports")}).Read()
	if portErr != nil || len(state.Instances) != 0 || len(state.Assignments) != 0 {
		t.Fatalf("fallback preflight mutated port state: %#v %v", state, portErr)
	}
}

func TestStartReusesSingletonWithoutColdStartPreflight(t *testing.T) {
	s := Store{Data: t.TempDir()}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='app'\n[commands.web]\nexec=['/bin/true']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canonical, canonicalErr := filepath.EvalSymlinks(root)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	root = canonical
	ps := ports.Store{Directory: filepath.Join(s.Data, "ports")}
	var instance ports.Instance
	if err := ps.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		created, failure := state.Register("app", root)
		if failure != nil {
			return false, failure
		}
		instance = *created
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	existing := Record{
		ID:        tasks.ID(),
		Profile:   "app",
		Instance:  instance.ID,
		Directory: root,
		Command:   "web",
		CreatedAt: started,
		StartedAt: &started,
		State:     "running",
	}
	if err := writePrivate(s.path(existing.ID, "record.json"), existing); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/status" || request.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(existing)
	}))
	defer server.Close()
	if err := writePrivate(s.path(existing.ID, "control.json"), control{ID: existing.ID, Address: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	called := 0
	result, err := s.start(context.Background(), Request{
		Action:    "start",
		Directory: root,
		Command:   "web",
		RequestID: tasks.ID(),
		BeforeStart: func(context.Context) *protocol.Error {
			called++
			return protocol.NewError("requirements_failed", "must not run", 3, nil)
		},
	}, "")
	if err != nil || result.Item.ID != existing.ID || result.Changed || called != 0 {
		t.Fatalf("singleton reuse drifted: result=%#v called=%d err=%v", result, called, err)
	}
}

func TestBoundedRawLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	if e := os.Chmod(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	w := &boundedLog{path: path}
	if _, e := w.Write([]byte(strings.Repeat("a", 2<<20))); e != nil {
		t.Fatal(e)
	}
	w.Write([]byte("end"))
	b, e := maintenance.Read(path, 1<<20)
	if e != nil || len(b) != 1<<20 || !strings.HasSuffix(string(b), "end") {
		t.Fatal(len(b), e)
	}
	i, _ := os.Stat(path)
	if i.Mode().Perm() != 0600 {
		t.Fatal(i.Mode())
	}
}

func TestErrorExitCodes(t *testing.T) {
	for code, want := range map[string]int{"invalid_argument": 2, "process_not_found": 3, "canceled": 130, "execution_failed": 126} {
		if err := failure(code); err.ExitCode != want {
			t.Errorf("%s exit code = %d, want %d", code, err.ExitCode, want)
		}
	}
}

func TestRestartBeforeStopFailurePreservesRunningExecution(t *testing.T) {
	s := Store{Data: t.TempDir()}
	started := time.Now().UTC()
	old := Record{ID: tasks.ID(), Profile: "app", Instance: "instance", Directory: t.TempDir(), Command: "web", CreatedAt: started, StartedAt: &started, State: "running"}
	if err := writePrivate(s.path(old.ID, "record.json"), old); err != nil {
		t.Fatal(err)
	}
	phases := []RestartPhase{}
	request := Request{
		Action:    "restart",
		ID:        old.ID,
		RequestID: tasks.ID(),
		RestartPreflight: func(_ context.Context, record Record, phase RestartPhase) *protocol.Error {
			phases = append(phases, phase)
			if record.ID != old.ID || record.Directory != old.Directory || record.Command != old.Command {
				t.Fatalf("restart preflight received stale record: %#v", record)
			}
			return protocol.NewError("requirements_failed", "pre-stop blocked", 3, nil)
		},
	}
	result, failure := s.Apply(context.Background(), request)
	if failure == nil || failure.Code != "requirements_failed" || result.Changed || result.Item.ID != "" {
		t.Fatalf("pre-stop failure mutated restart: %#v %v", result, failure)
	}
	if len(phases) != 1 || phases[0] != RestartBeforeStop {
		t.Fatalf("unexpected restart phases: %#v", phases)
	}
	stored, readErr := s.read(old.ID)
	if readErr != nil || stored.EndedAt != nil || stored.State != "running" {
		t.Fatalf("old execution changed after pre-stop failure: %#v %v", stored, readErr)
	}
}

func TestRestartPreservesStopMutationWhenColdStartPreflightFails(t *testing.T) {
	s := Store{Data: t.TempDir()}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='app'\n[commands.web]\nexec=['/bin/true']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = canonical

	started := time.Now().UTC()
	old := Record{ID: tasks.ID(), Profile: "app", Instance: "instance", Directory: root, Command: "web", CreatedAt: started, StartedAt: &started, State: "running"}
	if err := writePrivate(s.path(old.ID, "record.json"), old); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		switch request.URL.Path {
		case "/stop":
			ended := time.Now().UTC()
			stopped := old
			stopped.EndedAt = &ended
			stopped.State = "stopped"
			stopped.Reason = "stop_requested"
			if err := writePrivate(s.path(old.ID, "record.json"), stopped); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(old)
		case "/status":
			_ = json.NewEncoder(w).Encode(old)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	if err := writePrivate(s.path(old.ID, "control.json"), control{ID: old.ID, Address: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}

	phases := []RestartPhase{}
	request := Request{
		Action:    "restart",
		ID:        old.ID,
		RequestID: tasks.ID(),
		RestartPreflight: func(_ context.Context, record Record, phase RestartPhase) *protocol.Error {
			phases = append(phases, phase)
			if record.ID != old.ID {
				t.Fatalf("restart phase received wrong execution: %#v", record)
			}
			if phase == RestartBeforeStart {
				return protocol.NewError("requirements_failed", "cold start blocked", 3, nil)
			}
			return nil
		},
	}
	result, failure := s.Apply(context.Background(), request)
	if failure == nil || failure.Code != "requirements_failed" {
		t.Fatalf("expected preflight failure, got result=%#v err=%v", result, failure)
	}
	if !result.Changed || result.Item.ID != old.ID || result.Item.State != "stopped" {
		t.Fatalf("restart lost accepted stop mutation: %#v", result)
	}
	if len(phases) != 2 || phases[0] != RestartBeforeStop || phases[1] != RestartBeforeStart {
		t.Fatalf("unexpected restart phase order: %#v", phases)
	}
}

func writeLegacyProcessProof(t *testing.T, s Store, executionID, profile string, archivedAt time.Time, restored bool) string {
	t.Helper()
	archiveID := tasks.ID()
	dir := filepath.Join(s.Data, "archives", archiveID)
	if err := tasks.PrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	var restoredAt *time.Time
	if restored {
		at := archivedAt.Add(time.Hour)
		restoredAt = &at
	}
	entry := map[string]any{
		"id":          archiveID,
		"kind":        "completed_process",
		"profile":     profile,
		"source":      s.path(executionID, "record.json"),
		"bytes":       int64(128),
		"archived_at": archivedAt,
		"restored_at": restoredAt,
		"purged_at":   nil,
	}
	if err := tasks.WritePrivate(filepath.Join(dir, "entry.json"), entry); err != nil {
		t.Fatal(err)
	}
	return archiveID
}

func TestArchivedMarkerExcludesExecutionWithoutHidingCorruption(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	ended := time.Now().UTC().Add(-time.Hour)
	record := Record{
		ID:        id,
		Profile:   "app",
		Directory: t.TempDir(),
		Command:   "web",
		CreatedAt: ended.Add(-time.Hour),
		EndedAt:   &ended,
		State:     "stopped",
		Capture:   true,
	}
	if err := writePrivate(s.path(id, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	archiveID := tasks.ID()
	archivedAt := time.Now().UTC()
	if err := s.WriteArchiveMarker(record, archiveID, archivedAt); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.path(id, "record.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Status(context.Background(), id); err == nil || err.Code != "process_not_found" {
		t.Fatalf("archived status = %v", err)
	}
	if records, err := s.StoredRecords(); err != nil || len(records) != 0 {
		t.Fatalf("stored records = %#v %v", records, err)
	}
	if records, err := s.List(context.Background(), ""); err != nil || len(records) != 0 {
		t.Fatalf("listed records = %#v %v", records, err)
	}
	owner, err := s.ArchivedOwner(id)
	if err != nil || owner.Legacy || owner.Profile != "app" || owner.ArchiveID != archiveID || owner.EndedAt == nil || !owner.EndedAt.Equal(ended) || owner.Capture == nil || !*owner.Capture {
		t.Fatalf("archived owner = %#v %v", owner, err)
	}

	if err := s.RemoveArchiveMarker(id, archiveID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StoredRecords(); err == nil || err.Code != "io_error" {
		t.Fatalf("unmarked hole was accepted: %v", err)
	}
}

func TestLegacyArchiveProofExcludesPreMarkerExecutionHole(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	if err := tasks.PrivateDir(filepath.Join(s.root(), id)); err != nil {
		t.Fatal(err)
	}
	archivedAt := time.Now().UTC().Add(-48 * time.Hour)
	first := writeLegacyProcessProof(t, s, id, "app", archivedAt, false)
	_ = writeLegacyProcessProof(t, s, id, "app", archivedAt.Add(time.Hour), false)

	if _, err := s.Status(context.Background(), id); err == nil || err.Code != "process_not_found" {
		t.Fatalf("legacy archived status = %v", err)
	}
	if records, err := s.StoredRecords(); err != nil || len(records) != 0 {
		t.Fatalf("legacy proof did not exclude hole: %#v %v", records, err)
	}
	owner, err := s.ArchivedOwner(id)
	if err != nil || !owner.Legacy || owner.Profile != "app" || owner.ArchiveID != first || !owner.ArchivedAt.Equal(archivedAt) || owner.EndedAt != nil || owner.Capture != nil {
		t.Fatalf("legacy owner = %#v %v", owner, err)
	}
}

func TestRestoredOrConflictingLegacyProofDoesNotHideExecutionHole(t *testing.T) {
	for name, setup := range map[string]func(*testing.T, Store, string){
		"restored": func(t *testing.T, s Store, id string) {
			writeLegacyProcessProof(t, s, id, "app", time.Now().UTC().Add(-time.Hour), true)
		},
		"conflicting_profiles": func(t *testing.T, s Store, id string) {
			at := time.Now().UTC().Add(-2 * time.Hour)
			writeLegacyProcessProof(t, s, id, "app", at, false)
			writeLegacyProcessProof(t, s, id, "other", at.Add(time.Minute), false)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := Store{Data: t.TempDir()}
			id := tasks.ID()
			if err := tasks.PrivateDir(filepath.Join(s.root(), id)); err != nil {
				t.Fatal(err)
			}
			setup(t, s, id)
			if _, err := s.StoredRecords(); err == nil || err.Code != "io_error" {
				t.Fatalf("invalid legacy proof hid execution hole: %v", err)
			}
		})
	}
}

func TestArchiveMarkerMustMatchCoexistingRecord(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	ended := time.Now().UTC()
	record := Record{ID: id, Profile: "app", Directory: t.TempDir(), CreatedAt: ended.Add(-time.Hour), EndedAt: &ended, State: "stopped"}
	if err := writePrivate(s.path(id, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteArchiveMarker(record, tasks.ID(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record.Capture = true
	if err := writePrivate(s.path(id, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Status(context.Background(), id); err == nil || err.Code != "io_error" {
		t.Fatalf("mismatched marker was accepted: %v", err)
	}
}

func TestWriteArchiveMarkerRequiresCurrentExecution(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	ended := time.Now().UTC()
	record := Record{ID: id, Profile: "app", Directory: t.TempDir(), CreatedAt: ended.Add(-time.Hour), EndedAt: &ended, State: "stopped"}
	if err := s.WriteArchiveMarker(record, tasks.ID(), time.Now().UTC()); err == nil || err.Code != "revision_conflict" {
		t.Fatalf("marker was created without a current record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.root(), id)); !os.IsNotExist(err) {
		t.Fatalf("marker write created an execution directory: %v", err)
	}
}
