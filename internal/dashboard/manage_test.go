package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestManagementAuthenticationAndConcurrency(t *testing.T) {
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(t.TempDir(), "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
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
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(t.TempDir(), "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
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
	state, e := store.Read()
	if e != nil || state.Current(id) != nil || state.Runs[run.ID].State != "revoked" {
		t.Fatal("agent run remains active", e)
	}
}
