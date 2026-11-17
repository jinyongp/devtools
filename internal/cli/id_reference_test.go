package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProcessStatusResolvesUniquePrefix(t *testing.T) {
	a := testApp(t)
	store, condition := a.serviceStore()
	if condition != nil {
		t.Fatal(condition)
	}
	dir := privateTempDir(t)
	now := time.Now().UTC()
	id := "a4531000-0000-4000-8000-000000000001"
	insert := func(id string) {
		t.Helper()
		if err := tasks.PrivateDir(filepath.Join(store.Data, "processes", id)); err != nil {
			t.Fatal(err)
		}
		if err := store.RestoreRecord(services.Record{ID: id, Profile: "prefix", Directory: dir, Command: "prefix", State: "exited", CreatedAt: now, EndedAt: &now}); err != nil {
			t.Fatal(err)
		}
	}
	insert(id)
	if got := outputData(t, a, []string{"process", "status", "a453"})["item"].(map[string]any)["id"]; got != id {
		t.Fatal(got)
	}
	insert("a453b000-0000-4000-8000-000000000002")
	code, out, diagnostic := invoke(t, a, "", "process", "status", "a453")
	if code == 0 || out != "" || !strings.Contains(diagnostic, "ambiguous_id") || !strings.Contains(diagnostic, "a4531") || !strings.Contains(diagnostic, "a453b") {
		t.Fatal(code, out, diagnostic)
	}
}
