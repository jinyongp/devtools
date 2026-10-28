package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestDashboardCompletedStartRecoversAfterConfigLoss(t *testing.T) {
	data, root := privateTempDir(t), privateTempDir(t)
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(data, "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	id := tasks.ID()
	q := services.Request{Action: "start", Directory: root, Command: "web", RequestID: id}
	raw, _ := json.Marshal(q)
	digest := sha256.Sum256(raw)
	now := time.Now().UTC()
	value := map[string]any{"fingerprint": hex.EncodeToString(digest[:]), "done": true, "result": services.Result{Changed: true, Item: services.Record{ID: id, Profile: "app", Directory: root, Command: "web", CreatedAt: now, StartedAt: &now, State: "running"}}}
	path := filepath.Join(data, "processes", "receipts", id+".json")
	if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
		t.Fatal(e)
	}
	if e := tasks.WritePrivate(path, value); e != nil {
		t.Fatal(e)
	}
	invoke := func(profile, session, origin string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(actionRequest{Domain: "process", Profile: profile, Process: &q})
		r := httptest.NewRequest("POST", s.registry.Address+"/api/actions", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Authorization", "Bearer "+session)
		out := httptest.NewRecorder()
		s.ServeHTTP(out, r)
		return out
	}
	for _, config := range []string{"[broken\n", ""} {
		if config != "" {
			if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); e != nil {
				t.Fatal(e)
			}
		} else {
			if e := os.Remove(filepath.Join(root, "devtools.toml")); e != nil {
				t.Fatal(e)
			}
		}
		out := invoke("app", "session", s.registry.Address)
		if out.Code != 200 || !strings.Contains(out.Body.String(), `"replayed":true`) || out.Header().Get("Content-Type") != "application/json" {
			t.Fatal(out.Code, out.Body)
		}
	}
	if out := invoke("other", "session", s.registry.Address); out.Code == 200 {
		t.Fatal("cross-profile replay accepted")
	}
	if out := invoke("app", "wrong", s.registry.Address); out.Code == 200 {
		t.Fatal("invalid session replay accepted")
	}
	if out := invoke("app", "session", "https://other.invalid"); out.Code == 200 {
		t.Fatal("invalid Origin replay accepted")
	}
	q.Command = "different"
	if out := invoke("app", "session", s.registry.Address); out.Code != 409 {
		t.Fatal("changed payload accepted", out.Code, out.Body)
	}
}
