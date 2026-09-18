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
