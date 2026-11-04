package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProcessCurrentSummarizes313RecordsAndHistoryReachesEveryExecution(t *testing.T) {
	a := testApp(t)
	dir, other := privateTempDir(t), privateTempDir(t)
	t.Chdir(dir)
	if e := os.WriteFile("devtools.toml", []byte("profile='app-a'\n[ports.web]\n[ports.api]\n[commands.web]\nexec=['echo']\nserve=['web']\n[commands.api]\nexec=['echo']\nserve=['api']\n[commands.check]\nexec=['echo']\n"), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := a.serviceStore()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	var active services.Record
	for i := 0; i < 313; i++ {
		r := services.Record{ID: tasks.ID(), Profile: "app-a", Directory: dir, Command: "web", State: "exited", CreatedAt: now.Add(time.Duration(i-400) * time.Hour)}
		ended := r.CreatedAt.Add(time.Minute)
		r.EndedAt = &ended
		if i == 312 {
			r.State, r.CreatedAt, r.EndedAt = "starting", now, nil
			active = r
		}
		if e := tasks.PrivateDir(filepath.Join(s.Data, "processes", r.ID)); e != nil {
			t.Fatal(e)
		}
		if e := s.RestoreRecord(r); e != nil {
			t.Fatal(e)
		}
	}
	r := services.Record{ID: tasks.ID(), Profile: "app-a", Directory: other, Command: "retired", State: "failed", Reason: "fixture_failure", CreatedAt: now.Add(-time.Hour), EndedAt: &now}
	if e := tasks.PrivateDir(filepath.Join(s.Data, "processes", r.ID)); e != nil {
		t.Fatal(e)
	}
	if e := s.RestoreRecord(r); e != nil {
		t.Fatal(e)
	}
	// A live supervisor keeps this fixture independent of the time spent writing
	// historical records, which can exceed the legacy startup grace on Windows.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(active)
	}))
	t.Cleanup(server.Close)
	if e := tasks.WritePrivate(filepath.Join(s.Data, "processes", active.ID, "control.json"), map[string]string{"id": active.ID, "address": server.URL, "token": strings.Repeat("f", 64)}); e != nil {
		t.Fatal(e)
	}
	current := outputData(t, a, []string{"process", "list"})["items"].([]any)
	if len(current) != 2 {
		t.Fatal(current)
	}
	for _, v := range current {
		row := v.(map[string]any)
		if row["command"] == "web" && (row["active_count"] != float64(1) || row["current_state"] != "starting") {
			t.Fatal(row)
		}
		if row["command"] == "check" || row["directory"] != dir {
			t.Fatal(row)
		}
	}
	profileRows := outputData(t, a, []string{"process", "list", "--profile", "app-a"})["items"].([]any)
	if len(profileRows) != 3 {
		t.Fatal(profileRows)
	}
	cache := filepath.Join(privateTempDir(t), "history-cache")
	page := outputData(t, a, []string{"process", "list", "--view", "history", "--limit", "50", "--query-cache-dir", cache})
	seen := map[string]bool{}
	for {
		for _, v := range page["items"].([]any) {
			id := v.(map[string]any)["execution_id"].(string)
			if seen[id] {
				t.Fatal("duplicate", id)
			}
			seen[id] = true
		}
		if page["next_cursor"] == nil {
			break
		}
		action := previewAction(t, page)
		if action.State != "ready" {
			t.Fatal(action)
		}
		page = outputData(t, a, action.Argv[1:])
	}
	if len(seen) != 313 {
		t.Fatal(len(seen))
	}
	// Slow text queries may report progress on stderr. These assertions cover
	// collection rendering; progress and error-stream isolation have separate tests.
	code, text, diagnostic := invoke(t, a, "", "--format", "text", "process", "list", "--view", "history", "--state", "exited", "--limit", "1", "--query-cache-dir", cache)
	if code != 0 || !strings.Contains(text, "Exited") || strings.Contains(text, "Unrecognized") {
		t.Fatal(code, text, diagnostic)
	}
	wide := []string{"--format", "text", "process", "list", "--view", "history", "--state", "exited", "--limit", "1", "--query-cache-dir", cache}
	for _, column := range []string{"command", "mode", "state", "execution_id", "env", "profile", "directory", "reason", "active_count"} {
		wide = append(wide, "--columns", column)
	}
	code, text, diagnostic = invoke(t, a, "", wide...)
	if code != 0 || !strings.Contains(text, "managed") || !strings.Contains(text, "Exited") || strings.Contains(text, "Result\n") || strings.Contains(text, "legacy_unverified") {
		t.Fatalf("wide explicit columns fell back or exposed unselected fields: %d %s %s", code, text, diagnostic)
	}
	code, text, diagnostic = invoke(t, a, "", "--format", "text", "process", "list", "--view", "history", "--command", "missing", "--columns", "mode")
	if code != 0 || !strings.Contains(text, "No items.") || strings.Contains(text, "Result\n") {
		t.Fatalf("empty explicit columns lost the text contract: %d %s %s", code, text, diagnostic)
	}
}
