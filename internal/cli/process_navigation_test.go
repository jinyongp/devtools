package cli

import (
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
	for i := 0; i < 313; i++ {
		r := services.Record{ID: tasks.ID(), Profile: "app-a", Directory: dir, Command: "web", State: "exited", CreatedAt: now.Add(time.Duration(i-400) * time.Hour)}
		ended := r.CreatedAt.Add(time.Minute)
		r.EndedAt = &ended
		if i == 312 {
			r.State, r.CreatedAt, r.EndedAt = "starting", now, nil
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
	code, text, diagnostic := invoke(t, a, "", "--format", "text", "process", "list", "--view", "history", "--state", "exited", "--limit", "1", "--query-cache-dir", cache)
	if code != 0 || diagnostic != "" || !strings.Contains(text, "Exited") || strings.Contains(text, "Unrecognized") {
		t.Fatal(code, text, diagnostic)
	}
}
