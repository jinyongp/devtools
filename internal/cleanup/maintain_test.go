package cleanup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/retainedbody"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestBoundedMaintenanceKeepsLivePeerSummaryAndReplay(t *testing.T) {
	root := privateTestRoot(t)
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")}
	s := services.Store{Data: e.Data}
	ctx := context.Background()
	now := time.Now().UTC()
	ended := now.Add(-31 * 24 * time.Hour)
	old := services.Record{ID: tasks.ID(), Profile: "app-a", Directory: root, Command: "api", Instance: "shared", CreatedAt: ended.Add(-time.Hour), EndedAt: &ended, State: "stopped", Capture: true}
	live := services.Record{ID: tasks.ID(), Profile: old.Profile, Directory: root, Command: "web", Instance: old.Instance, CreatedAt: now, State: "running"}
	for _, r := range []services.Record{old, live} {
		if err := s.RestoreRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(e.Data, "processes", old.ID, "output.log")
	if err := maintenance.Write(log, []byte("expired capture")); err != nil {
		t.Fatal(err)
	}
	q := services.Request{Action: "stop", ID: old.ID, RequestID: tasks.ID()}
	before, pe := s.Apply(ctx, q)
	if pe != nil {
		t.Fatal(pe)
	}
	preview, pe := e.Maintain(ctx, "app-a", 4, true)
	if pe != nil || preview.Changed != 0 || preview.Examined > 4 {
		t.Fatal(preview, pe)
	}
	if _, err := os.Stat(log); err != nil {
		t.Fatal("dry run removed log", err)
	}
	if _, err := os.Stat(filepath.Join(e.Data, ".retention", "position.json")); !os.IsNotExist(err) {
		t.Fatal("dry run saved position", err)
	}
	for range 3 {
		result, pe := e.Maintain(ctx, "app-a", 4, false)
		if pe != nil || result.Examined > 4 {
			t.Fatal(result, pe)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("expired log survived", err)
	}
	history, pe := s.Navigation(ctx, services.NavigationSelection{Profile: "app-a", History: true}, []services.ServiceDeclaration{})
	if pe != nil {
		t.Fatal(pe)
	}
	for _, row := range history {
		if row.ExecutionID == old.ID {
			t.Fatal("expired detail in history")
		}
	}
	current, pe := s.Navigation(ctx, services.NavigationSelection{Profile: "app-a"}, []services.ServiceDeclaration{{Profile: "app-a", Directory: root, Command: "api", Served: true}})
	if pe != nil || len(current) != 2 {
		t.Fatal(current, pe)
	}
	after, pe := s.Apply(ctx, q)
	if pe != nil || !after.Replayed {
		t.Fatal(after, pe)
	}
	before.Replayed = false
	after.Replayed = false
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("maintenance changed frozen replay")
	}
}

func TestMaintenanceUsesRecordedEndAndKeepsUnresolvedResult(t *testing.T) {
	root := privateTestRoot(t)
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")}
	s := services.Store{Data: e.Data}
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	recent := now.Add(-6 * 24 * time.Hour)
	rows := []services.Record{{ID: tasks.ID(), Profile: "app-a", Directory: root, Command: "missing", CreatedAt: old, State: "unknown"}, {ID: tasks.ID(), Profile: "app-a", Directory: root, Command: "failed", CreatedAt: old, EndedAt: &old, State: "failed"}, {ID: tasks.ID(), Profile: "app-a", Directory: root, Command: "recent", CreatedAt: old, EndedAt: &recent, State: "stopped", Capture: true}}
	for _, r := range rows {
		if err := s.RestoreRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(e.Data, "processes", rows[2].ID, "output.log")
	if err := maintenance.Write(log, []byte("within 7 days")); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, pe := e.Maintain(context.Background(), "app-a", 4, false); pe != nil {
			t.Fatal(pe)
		}
	}
	stored, pe := s.StoredMetadata()
	if pe != nil {
		t.Fatal(pe)
	}
	for _, r := range stored {
		if r.DetailRetired {
			t.Fatal("unsafe retirement", r.ID)
		}
	}
	if _, err := os.Stat(log); err != nil {
		t.Fatal("live log deleted", err)
	}
}

func TestMaintenanceBudgetIncludesBodiesAndSingleItemPassResumes(t *testing.T) {
	root := privateTestRoot(t)
	e := Engine{Data: filepath.Join(root, "data")}
	s := retainedbody.Store{Root: e.Data}
	ctx := context.Background()
	old := time.Now().Add(-31 * 24 * time.Hour)
	for n := range 5 {
		ref, err := s.Put(ctx, "archive_payload", []byte{byte(n)})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(e.Data, "retained-bodies", ref.Digest), old, old); err != nil {
			t.Fatal(err)
		}
	}
	removed := 0
	for range 12 {
		result, condition := e.Maintain(ctx, "", 1, false)
		if condition != nil || result.Examined > 1 || result.Changed > 1 {
			t.Fatal(result, condition)
		}
		removed += result.ReclaimedBodies
	}
	if removed != 5 {
		t.Fatal("single-item passes starved body reclamation", removed)
	}
	if dirs := (Engine{}).ownedCacheDirectories(); len(dirs) != 0 {
		t.Fatal("missing cache selected cwd", dirs)
	}
}
