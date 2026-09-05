package cleanup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
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
	if _, err = e.Purge(ctx, id); err == nil || err.Code != "retention_active" {
		t.Fatal(err)
	}
	if _, err = e.Restore(ctx, id); err != nil {
		t.Fatal(err)
	}
	restored, er := os.ReadFile(path)
	if er != nil || string(restored) != string(other) {
		t.Fatal("restore mismatch", er)
	}
	if _, err = e.Restore(ctx, id); err != nil {
		t.Fatal(err)
	}
	var a Archive
	tasks.ReadPrivate(e.archivePath(id, "entry.json"), &a)
	a.ArchivedAt = time.Now().Add(-31 * 24 * time.Hour)
	write(e.archivePath(id, "entry.json"), a)
	if _, err = e.Purge(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Restore(ctx, id); err == nil || err.Code != "archive_purged" {
		t.Fatal(err)
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
