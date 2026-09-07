package dashboard

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentAssetsReadOnEachRequest(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, assetRoot: root}
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		out := httptest.NewRecorder()
		s.ServeHTTP(out, httptest.NewRequest("GET", s.registry.Address+path, nil))
		return out
	}
	for _, content := range []string{"first version", "edited version"} {
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		out := get("/")
		if out.Code != 200 || out.Body.String() != content || out.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("live response: %d %s %v", out.Code, out.Body, out.Header())
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "new.js"), []byte("new file"), 0600); err != nil {
		t.Fatal(err)
	}
	if out := get("/new.js"); out.Code != 200 || out.Body.String() != "new file" {
		t.Fatal(out.Code, out.Body)
	}
	outside := filepath.Join(t.TempDir(), "private.js")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.js")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/escape.js", "/../private.js", "/.env", "/missing.js"} {
		if out := get(path); out.Code != 404 {
			t.Fatalf("%s: %d", path, out.Code)
		}
	}
	s.assetRoot = nil
	if out := get("/"); out.Code != 200 || !strings.Contains(out.Body.String(), "<!doctype html>") {
		t.Fatal("embedded dashboard changed", out.Code)
	}
}
