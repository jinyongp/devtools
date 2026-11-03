package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectionSnapshotContinuationIsIndependentAndBounded(t *testing.T) {
	parent := privateTempDir(t)
	data, cache := filepath.Join(parent, "data"), filepath.Join(parent, "cache")
	if e := os.Mkdir(data, 0700); e != nil {
		t.Fatal(e)
	}
	p := CollectionPager{DataRoot: data, CacheDirectory: cache}
	rows := []any{}
	for i := 0; i < 10000; i++ {
		rows = append(rows, map[string]any{"id": fmt.Sprint(i)})
	}
	first, e := p.First(context.Background(), "root/scope/filter/sort", Object{"selection": Object{"profile": "app-a"}}, rows, 50)
	if e != nil {
		t.Fatal(e)
	}
	// Cursor reads must neither open active storage nor publish/prune snapshots.
	if e := os.Remove(data); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(data, []byte("unreadable as a directory"), 0600); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	page := first
	for {
		items := page["items"].([]any)
		if len(items) > 50 {
			t.Fatal(len(items))
		}
		for _, value := range items {
			id := value.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatal("duplicate", id)
			}
			seen[id] = true
		}
		cursor, _ := page["next_cursor"].(string)
		if cursor == "" {
			break
		}
		page, e = p.Continue("root/scope/filter/sort", cursor, 50)
		if e != nil {
			t.Fatal(e)
		}
	}
	if len(seen) != 10000 {
		t.Fatal(len(seen))
	}
	if _, e := p.Continue("other-root-or-filter", first["next_cursor"].(string), 50); e == nil || e.Code != "cursor_invalid" {
		t.Fatal(e)
	}
}

func TestSingleCollectionPageCreatesNoCache(t *testing.T) {
	parent := privateTempDir(t)
	cache := filepath.Join(parent, "absent-cache")
	p := CollectionPager{DataRoot: filepath.Join(parent, "data"), CacheDirectory: cache}
	if _, e := p.First(context.Background(), "fp", Object{}, []any{}, 50); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(cache); !os.IsNotExist(e) {
		t.Fatal("single page created cache", e)
	}
}
