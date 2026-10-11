package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ca"
)

func TestNetworkViewRequiresSessionAndDoesNotInitializeState(t *testing.T) {
	root := filepath.Join(privateTempDir(t), "isolated")
	t.Setenv("DEVTOOLS_ISOLATED_ROOT", root)
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(root, "data", "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	read := func(auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", s.registry.Address+"/api/network?profile=app", nil)
		if auth {
			r.Header.Set("Authorization", "Bearer session")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := read(false); w.Code != 401 {
		t.Fatal(w.Code, w.Body)
	}
	if w := read(true); w.Code != 200 || !strings.Contains(w.Body.String(), `"isolated":true`) {
		t.Fatal(w.Code, w.Body)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("read initialized isolated state", err)
	}
	authority, _, e := (ca.Store{Data: filepath.Join(root, "data")}).Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	w := read(true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), authority.Fingerprint()) || strings.Contains(w.Body.String(), "PRIVATE KEY") || strings.Contains(w.Body.String(), "provider_owner") {
		t.Fatal(w.Code, w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"trust", "hosts.apply", "setup", "teardown"} {
		if _, e := s.networkAction(context.Background(), networkRequest{Action: action, RequestID: "11111111-1111-4111-8111-111111111111"}); e == nil {
			t.Fatal("OS action accepted", action)
		}
	}
}
