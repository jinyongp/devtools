package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/jinyongp/devtools/internal/tasks"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueryCacheInvalidatesOnMutation(t *testing.T) {
	store := tasks.Store{Directory: filepath.Join(t.TempDir(), "tasks"), Profile: "test"}
	add := func(title string) {
		_, e := store.Execute(context.Background(), tasks.Request{Action: "task.add", Body: tasks.Object{"title": title}, Options: map[string]string{"request-id": tasks.ID()}})
		if e != nil {
			t.Fatal(e)
		}
	}
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: store.Directory, sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	read := func() map[string]any {
		req := httptest.NewRequest("GET", s.registry.Address+"/api/query?profile=test&command=list", nil)
		req.AddCookie(&http.Cookie{Name: "devtools_session", Value: "session"})
		out := httptest.NewRecorder()
		s.ServeHTTP(out, req)
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body)
		}
		var body map[string]any
		if e := json.Unmarshal(out.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		return body
	}
	add("One")
	first := read()
	_ = read()
	add("Two")
	next := read()
	if first["revision"] == next["revision"] || len(next["items"].([]any)) != 2 {
		t.Fatal("stale cached projection")
	}
}

func TestPinnedBundle(t *testing.T) {
	var manifest map[string]struct {
		Version string         `json:"version"`
		SHA     string         `json:"sha256"`
		Peers   map[string]any `json:"peerDependencies"`
	}
	raw, e := assets.ReadFile("assets/dependencies.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &manifest); e != nil {
		t.Fatal(e)
	}
	bundle, e := assets.ReadFile("assets/d3.min.js")
	if e != nil {
		t.Fatal(e)
	}
	d3 := manifest["d3"]
	if d3.Version != "7.9.0" || len(d3.Peers) != 0 || fmt.Sprintf("%x", sha256.Sum256(bundle)) != d3.SHA {
		t.Fatal("bundle differs from exact dependency manifest")
	}
}

func TestSessionAndReadBoundary(t *testing.T) {
	s := &Server{registry: Registry{ID: "test", Address: "http://127.0.0.1:1234", Token: token()}, data: t.TempDir(), boot: map[string]time.Time{}, sessions: map[string]time.Time{}, stop: func() {}}
	invoke := func(method, path, body, origin, host string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, s.registry.Address+path, strings.NewReader(body))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if host != "" {
			req.Host = host
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		out := httptest.NewRecorder()
		s.ServeHTTP(out, req)
		return out
	}
	if r := invoke("GET", "/api/profiles", "", "", "", nil); r.Code != 401 {
		t.Fatal(r.Code)
	}
	key := token()
	s.boot[key] = time.Now().Add(time.Minute)
	body, _ := json.Marshal(map[string]string{"token": key})
	r := invoke("POST", "/session", string(body), s.registry.Address, "", nil)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	cookie := r.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie protections")
	}
	if r = invoke("POST", "/session", string(body), s.registry.Address, "", nil); r.Code != 401 {
		t.Fatal("bootstrap reused")
	}
	if r = invoke("GET", "/api/profiles", "", "", "", cookie); r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	if r = invoke("GET", "/api/profiles", "", "https://other.example", "", cookie); r.Code != 403 {
		t.Fatal("cross-origin allowed")
	}
	if r = invoke("GET", "/api/profiles", "", "", "other.example", cookie); r.Code != 403 {
		t.Fatal("host allowed")
	}
	if r = invoke("POST", "/api/query", "{}", s.registry.Address, "", cookie); r.Code != 405 {
		t.Fatal("mutation allowed")
	}
	if r = invoke("GET", "/admin", "", "", "", cookie); r.Code != 403 {
		t.Fatal("browser administration")
	}
	s.sessions[cookie.Value] = time.Now().Add(-time.Second)
	if r = invoke("GET", "/api/profiles", "", "", "", cookie); r.Code != 401 {
		t.Fatal("expired session")
	}
	if r = invoke("GET", "/d3.min.js", "", "", "", nil); r.Code != 200 || !strings.Contains(r.Body.String(), "v7.9.0") {
		t.Fatal("embedded D3 missing")
	}
}
