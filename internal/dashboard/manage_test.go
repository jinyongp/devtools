package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
