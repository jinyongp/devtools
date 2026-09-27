package cleanup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	profilecatalog "github.com/jinyongp/devtools/internal/profiles"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestPreviewConflictArchiveRestoreAndPurge(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	ctx := context.Background()
	path := filepath.Join(e.Cache, "task-queries", tasks.ID()+".json")
	b, _ := json.Marshal(map[string]any{"expires": time.Now().Add(-time.Hour), "items": []any{}})
	if er := maintenance.Write(path, b); er != nil {
		t.Fatal(er)
	}
	plan, err := e.Preview(ctx, "")
	if err != nil || len(plan.Items) != 1 {
		t.Fatal(plan, err)
	}
	other := append(append([]byte{}, b...), byte(' '))
	maintenance.Write(path, other)
	if _, err = e.Apply(ctx, plan.ID, []string{plan.Items[0].ID}, tasks.ID()); err == nil || err.Code != "revision_conflict" {
		t.Fatal(err)
	}
	plan, err = e.Preview(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	id := plan.Items[0].ID
	request := tasks.ID()
	result, err := e.Apply(ctx, plan.ID, []string{id}, request)
	if err != nil || len(result.Archives) != 1 {
		t.Fatal(result, err)
	}
	if _, er := os.Stat(path); !os.IsNotExist(er) {
		t.Fatal("source retained", er)
	}
	result, err = e.Apply(ctx, plan.ID, []string{id}, request)
	if err != nil || !result.Replayed {
		t.Fatal(result, err)
	}
	if _, changed, err := e.Purge(ctx, id); err == nil || err.Code != "retention_active" || changed {
		t.Fatal(changed, err)
	}
	if _, changed, err := e.Restore(ctx, id); err != nil || !changed {
		t.Fatal(changed, err)
	}
	restored, er := os.ReadFile(path)
	if er != nil || string(restored) != string(other) {
		t.Fatal("restore mismatch", er)
	}
	if _, changed, err := e.Restore(ctx, id); err != nil || changed {
		t.Fatal(changed, err)
	}
	var a Archive
	tasks.ReadPrivate(e.archivePath(id, "entry.json"), &a)
	a.ArchivedAt = time.Now().Add(-31 * 24 * time.Hour)
	write(e.archivePath(id, "entry.json"), a)
	if _, changed, err := e.Purge(ctx, id); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if _, changed, err := e.Purge(ctx, id); err != nil || changed {
		t.Fatal(changed, err)
	}
	if _, changed, err := e.Restore(ctx, id); err == nil || err.Code != "archive_purged" || changed {
		t.Fatal(changed, err)
	}
}
func TestActiveTaskProfileRetained(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")}
	s := tasks.Store{Directory: filepath.Join(e.Data, "tasks"), Profile: "test"}
	_, err := s.Execute(context.Background(), tasks.Request{Action: "task.add", Options: map[string]string{"request-id": tasks.ID()}, Body: tasks.Object{"title": "active"}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.Preview(context.Background(), "test")
	if err != nil || len(plan.Items) != 0 {
		t.Fatal(plan, err)
	}
}

func TestErrorExitCodes(t *testing.T) {
	engine := Engine{Data: t.TempDir(), Cache: t.TempDir()}
	if _, err := engine.Apply(context.Background(), "bad-id", []string{"bad-id"}, "bad-id"); err == nil || err.Code != "invalid_argument" || err.ExitCode != 2 {
		t.Fatal("invalid cleanup input contract", err)
	}
	if _, _, err := engine.Restore(context.Background(), tasks.ID()); err == nil || err.Code != "archive_not_found" || err.ExitCode != 3 {
		t.Fatal("missing archive contract", err)
	}
	for code, want := range map[string]int{"invalid_argument": 2, "revision_conflict": 3, "storage_error": 1} {
		if err := fail(code); err.ExitCode != want {
			t.Errorf("%s exit code = %d, want %d", code, err.ExitCode, want)
		}
	}
}

func TestCompletedProcessCleanupPreservesProfileEnumeration(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	id := tasks.ID()
	ended := time.Now().UTC().Add(-31 * 24 * time.Hour)
	record := services.Record{
		ID:        id,
		Profile:   "archived",
		Directory: filepath.Join(root, "project"),
		Command:   "web",
		CreatedAt: ended.Add(-time.Hour),
		EndedAt:   &ended,
		State:     "stopped",
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Write(filepath.Join(engine.Data, "processes", id, "record.json"), body); err != nil {
		t.Fatal(err)
	}

	plan, failure := engine.Preview(context.Background(), "archived")
	if failure != nil {
		t.Fatal(failure)
	}
	var processItem Item
	for _, item := range plan.Items {
		if item.Kind == "completed_process" {
			processItem = item
			break
		}
	}
	if processItem.ID == "" {
		t.Fatalf("completed process candidate missing: %#v", plan.Items)
	}
	if _, failure := engine.Apply(context.Background(), plan.ID, []string{processItem.ID}, tasks.ID()); failure != nil {
		t.Fatal(failure)
	}
	if _, err := os.Stat(filepath.Join(engine.Data, "processes", id, "record.json")); !os.IsNotExist(err) {
		t.Fatalf("record was not retired: %v", err)
	}

	if items, failure := (services.Store{Data: engine.Data}).List(context.Background(), "archived"); failure != nil || len(items) != 0 {
		t.Fatalf("process list failed after legacy-style cleanup: %#v %v", items, failure)
	}
	names, failure := (profilecatalog.Catalog{Data: engine.Data}).Names()
	if failure != nil {
		t.Fatalf("profile enumeration failed after process cleanup: %v", failure)
	}
	if len(names) != 0 {
		t.Fatalf("archived process leaked into active profile names: %#v", names)
	}
}
