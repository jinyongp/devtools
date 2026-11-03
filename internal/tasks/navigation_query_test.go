package tasks

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTaskNavigationCombinesOrStatesAndSnapshotFilters(t *testing.T) {
	s := fixture(t)
	first := itemID(call(t, s, "task.add", "", Object{"title": "match alpha"}))
	second := itemID(call(t, s, "task.add", "", Object{"title": "match beta"}))
	call(t, s, "task.add", "", Object{"title": "other", "description": "match is private body content, not a name"})
	call(t, s, "task.cancel", second, Object{"reason": "Fixture canceled task."})
	q := Query{Command: "list", Options: map[string]string{"query": "MATCH", "sort": "name", "limit": "1", "query-cache-dir": filepath.Join(privateTempDir(t), "snapshots")}, ListOptions: map[string][]string{"state": {"open", "canceled"}}}
	page, e := s.Query(context.Background(), q)
	if e != nil || len(page["items"].([]any)) != 1 || str(objectValue(page["items"].([]any)[0]), "id") != first {
		t.Fatal(page, e)
	}
	call(t, s, "task.add", "", Object{"title": "match added after snapshot"})
	q.Options["cursor"] = str(page, "next_cursor")
	q.ListOptions["state"] = []string{"canceled", "open"}
	page, e = s.Query(context.Background(), q)
	if e != nil || len(page["items"].([]any)) != 1 || str(objectValue(page["items"].([]any)[0]), "id") != second || page["next_cursor"] != nil {
		t.Fatal(page, e)
	}
	q.Options["query"] = "changed"
	if _, e := s.Query(context.Background(), q); e == nil || e.Code != "cursor_invalid" {
		t.Fatal(e)
	}
}
