package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestManagementAuthenticationAndConcurrency(t *testing.T) {
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(privateTempDir(t), "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	call := func(body, origin, credential string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", s.registry.Address+"/api/actions", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Authorization", "Bearer "+credential)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	body := `{"domain":"task","profile":"app","action":"task.add","body":{"title":"One"},"options":{"request-id":"00000000-0000-4000-8000-000000000001","if-revision":"0"}}`
	for _, tc := range []struct {
		origin, credential string
		status             int
	}{{s.registry.Address, "", 401}, {"", "session", 403}, {"http://evil.invalid", "session", 403}} {
		if r := call(body, tc.origin, tc.credential); r.Code != tc.status {
			t.Fatal(r.Code, r.Body)
		}
	}
	for _, bad := range []string{body + " {}", strings.Replace(body, `"task.add"`, `"shell.run"`, 1), strings.Replace(body, `"if-revision":"0"`, `"unknown":"0"`, 1)} {
		if r := call(bad, s.registry.Address, "session"); r.Code != 400 {
			t.Fatal(r.Code, r.Body)
		}
	}
	for _, action := range []string{"run.claimed", "run.taken_over", "run.resumed", "run.checkpointed", "run.released", "task.completed", "validation.record", "validation.accept"} {
		if r := call(strings.Replace(body, "task.add", action, 1), s.registry.Address, "session"); r.Code != 400 {
			t.Fatalf("execution action %s accepted: %d", action, r.Code)
		}
	}
	r := call(body, s.registry.Address, "session")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	r = call(body, s.registry.Address, "session")
	var replay map[string]any
	json.Unmarshal(r.Body.Bytes(), &replay)
	if r.Code != 200 || replay["data"].(map[string]any)["replayed"] != true {
		t.Fatal(r.Code, r.Body)
	}
	stale := strings.Replace(body, "000000000001", "000000000002", 1)
	if r = call(stale, s.registry.Address, "session"); r.Code != 409 {
		t.Fatal(r.Code, r.Body)
	}
	conflict := strings.Replace(body, "One", "Two", 1)
	if r = call(conflict, s.registry.Address, "session"); r.Code != 409 {
		t.Fatal(r.Code, r.Body)
	}
}

func TestDashboardRevokesObservedAgentRun(t *testing.T) {
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(privateTempDir(t), "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	store := tasks.Store{Directory: s.data, Profile: "app"}
	added, e := store.Execute(context.Background(), tasks.Request{Action: "task.add", Body: tasks.Object{"title": "Agent task"}, Options: map[string]string{"request-id": tasks.ID()}})
	if e != nil {
		t.Fatal(e)
	}
	id := added["item"].(tasks.Object)["id"].(string)
	claimed, e := store.Execute(context.Background(), tasks.Request{Action: "run.claimed", Target: id, Options: map[string]string{"request-id": tasks.ID()}})
	if e != nil {
		t.Fatal(e)
	}
	run := claimed["run"].(*tasks.Run)
	// A valid agent completion must still be refused by the dashboard boundary.
	completion, _ := json.Marshal(actionRequest{Domain: "task", Profile: "app", Action: "task.completed", Target: id, Body: tasks.Object{"summary": "Completed by browser"}, Options: map[string]string{"request-id": tasks.ID(), "if-revision": fmt.Sprint(claimed["revision"]), "context": claimed["context"].(string)}})
	blocked := httptest.NewRequest("POST", s.registry.Address+"/api/actions", strings.NewReader(string(completion)))
	blocked.Header.Set("Content-Type", "application/json")
	blocked.Header.Set("Origin", s.registry.Address)
	blocked.Header.Set("Authorization", "Bearer session")
	denied := httptest.NewRecorder()
	s.ServeHTTP(denied, blocked)
	if denied.Code != 400 {
		t.Fatalf("dashboard accepted agent completion: %d", denied.Code)
	}
	body, _ := json.Marshal(actionRequest{Domain: "task", Profile: "app", Action: "run.revoked", Target: id, Body: tasks.Object{"reason": "Session ended"}, Options: map[string]string{"request-id": tasks.ID(), "if-revision": fmt.Sprint(claimed["revision"]), "expected-run": run.ID}})
	r := httptest.NewRequest("POST", s.registry.Address+"/api/actions", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", s.registry.Address)
	r.Header.Set("Authorization", "Bearer session")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), claimed["context"].(string)) {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	state, e := store.Read(context.Background())
	if e != nil || state.Current(id) != nil || state.Runs[run.ID].State != "revoked" {
		t.Fatal("agent run remains active", e)
	}
}

func TestDashboardProcessRestartPreStopValidationPreservesRunningExecution(t *testing.T) {
	data := privateTempDir(t)
	root := privateTempDir(t)
	initial := "profile='app'\n[commands.web]\nexec=['/bin/true']\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, failure := project.Resolve(root, "")
	if failure != nil {
		t.Fatal(failure)
	}
	started := time.Now().UTC()
	id := tasks.ID()
	record := services.Record{
		ID:        id,
		Profile:   "app",
		Instance:  "instance",
		Directory: resolved.Root,
		Command:   "web",
		CreatedAt: started,
		StartedAt: &started,
		State:     "running",
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(data, "processes", id, "record.json")
	if err := maintenance.Write(recordPath, body); err != nil {
		t.Fatal(err)
	}
	changed := "profile='app'\n[commands.web]\nexec=['/definitely-not-an-installed-review-executable']\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}

	server := &Server{
		registry: Registry{Address: "http://127.0.0.1:1234"},
		data:     filepath.Join(data, "tasks"),
		sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)},
	}
	requestBody, err := json.Marshal(actionRequest{
		Domain:  "process",
		Profile: "app",
		Process: &services.Request{Action: "restart", ID: id, RequestID: tasks.ID()},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", server.registry.Address+"/api/actions", strings.NewReader(string(requestBody)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.registry.Address)
	request.Header.Set("Authorization", "Bearer session")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "\"code\":\"requirements_failed\"") {
		t.Fatalf("restart response: %d %s", response.Code, response.Body.String())
	}
	storedBytes, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored services.Record
	if err := json.Unmarshal(storedBytes, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != id || stored.EndedAt != nil || stored.State != "running" {
		t.Fatalf("dashboard pre-stop validation stopped the old process: %#v", stored)
	}
}

func TestDashboardValuesRequestCancelsWhileMaintenanceLocked(t *testing.T) {
	root := privateTempDir(t)
	server := &Server{
		registry: Registry{Address: "http://127.0.0.1:1234"},
		data:     filepath.Join(root, "tasks"),
		sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)},
	}
	unlock, err := maintenance.Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest("GET", server.registry.Address+"/api/values?profile=app", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer session")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.ServeHTTP(response, request)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("dashboard values request crossed maintenance lock before cancellation")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("dashboard values request did not observe canceled context")
	}
	if response.Code != 400 || !strings.Contains(response.Body.String(), "\"code\":\"canceled\"") {
		t.Fatalf("canceled values response: %d %s", response.Code, response.Body.String())
	}
}
