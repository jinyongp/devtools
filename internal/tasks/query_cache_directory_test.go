package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueryCacheSelectionAndIndependentContinuation(t *testing.T) {
	s := fixture(t)
	for n := 0; n < 3; n++ {
		call(t, s, "task.add", "", Object{"title": "cache fixture"})
	}
	cache := filepath.Join(privateTempDir(t), "private-cache")
	q := Query{Command: "list", Options: map[string]string{"query-cache-dir": cache, "limit": "1"}}
	first, e := s.Query(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	q.Options["cursor"] = str(first, "next_cursor")
	// A cursor is entirely independent of the data root and its locks/WAL.
	old := s.Directory
	s.Directory = filepath.Join(privateTempDir(t), "missing", "tasks")
	continued, e := s.Query(context.Background(), q)
	if e != nil || continued["revision"] != first["revision"] {
		t.Fatal("cursor touched missing data", e)
	}
	q.Options["query-cache-dir"] = filepath.Join(privateTempDir(t), "other")
	if _, e := s.Query(context.Background(), q); e == nil || e.Code != "cursor_invalid" {
		t.Fatal(e)
	}
	s.Directory, s.Cache = old, cache
	delete(q.Options, "query-cache-dir")
	if _, e := s.Query(context.Background(), q); e != nil {
		t.Fatal("cache selection affected fingerprint", e)
	}
	q.Options["query-cache-dir"] = cache
	if _, e := s.Query(context.Background(), q); e != nil {
		t.Fatal(e)
	}
}

func TestQueryCacheRejectsOverlapAndDefersFilesystemUse(t *testing.T) {
	s := fixture(t)
	root := filepath.Dir(s.Directory)
	for _, path := range []string{"", "relative", root, filepath.Dir(root), filepath.Join(root, "profiles"), filepath.Join(root, ".maintenance"), s.Directory} {
		if _, e := s.Query(context.Background(), Query{Command: "next", Options: map[string]string{"query-cache-dir": path}}); e == nil || e.Code != "invalid_argument" {
			t.Fatal(path, e)
		}
	}
	cache := filepath.Join(privateTempDir(t), "absent")
	if _, e := s.Query(context.Background(), Query{Command: "next", Options: map[string]string{"query-cache-dir": cache}}); e != nil {
		t.Fatal(e)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("single query created cache")
	}
}

func TestQueryCachePruneRecognizesOnlyExpiredSnapshots(t *testing.T) {
	s := fixture(t)
	s.Cache = filepath.Join(privateTempDir(t), "cache")
	if err := PrivateDir(s.Cache); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Hour)
	live := time.Now().Add(time.Hour)
	q := Query{Command: "list"}
	names := map[string]bool{}
	write := func(name string, value any, remove bool) {
		t.Helper()
		if err := WritePrivate(filepath.Join(s.Cache, name), value); err != nil {
			t.Fatal(err)
		}
		names[name] = remove
	}
	legacy := ID() + ".json"
	write(legacy, Snapshot{Projection: ProjectionVersion, Fingerprint: queryFingerprint(s.Profile, q), Expires: expired, Revision: 1, Items: []any{}}, true)
	graph := GraphSnapshot{Version: graphSnapshotVersion, Projection: ProjectionVersion, Profile: s.Profile, Kind: "task", Expires: expired, Revision: 1, Nodes: map[string]Object{}}
	write(ID()+".json", graph, true)
	write(ID()+".json", Object{"expires": expired}, false)
	write("unrelated.json", Snapshot{Projection: ProjectionVersion, Fingerprint: "known", Expires: expired, Revision: 1}, false)
	indexed := ID() + ".json"
	if err := writeQueryCache(filepath.Join(s.Cache, indexed), s.Profile, Snapshot{Projection: ProjectionVersion, Fingerprint: "known", Expires: expired, Revision: 1, Items: []any{Object{}}}); err != nil {
		t.Fatal(err)
	}
	names[indexed] = true
	write(ID()+".json", Snapshot{Projection: ProjectionVersion, Fingerprint: "known", Expires: live, Revision: 1}, false)
	for name := range names {
		if err := os.Chtimes(filepath.Join(s.Cache, name), expired, expired); err != nil {
			t.Fatal(err)
		}
	}
	// Non-paged queries never prune, including expired owned files.
	if _, e := s.Query(context.Background(), Query{Command: "next"}); e != nil {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(s.Cache, legacy)); err != nil {
		t.Fatal("query pruned cache", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	s.pruneQueryCaches(canceled)
	if _, err := os.Stat(filepath.Join(s.Cache, legacy)); err != nil {
		t.Fatal("canceled prune removed file", err)
	}
	// A newly published page activates the narrow best-effort cleanup.
	if _, e := s.page(q, Object{"revision": 1}, []any{Object{}, Object{}}, 1); e != nil {
		t.Fatal(e)
	}
	for name, removed := range names {
		_, err := os.Stat(filepath.Join(s.Cache, name))
		if os.IsNotExist(err) != removed {
			t.Fatal(name, "wrong prune decision", err)
		}
	}
}

func TestGraphCacheSelectionRetainsFrozenRevision(t *testing.T) {
	s := fixture(t)
	for n := 0; n < 3; n++ {
		call(t, s, "task.add", "", Object{"title": "graph fixture"})
	}
	cache := filepath.Join(privateTempDir(t), "graph-cache")
	q := Query{Command: "tree", Options: map[string]string{"query-cache-dir": cache, "limit": "1"}}
	first, e := s.Query(context.Background(), q)
	if e != nil || first["truncated"] != true {
		t.Fatal(first, e)
	}
	q.Options["cursor"] = str(first, "cursor")
	q.Options["limit"] = "200"
	s.Directory = filepath.Join(privateTempDir(t), "absent", "tasks")
	continued, e := s.Query(context.Background(), q)
	if e != nil || continued["revision"] != first["revision"] || continued["truncated"] != false {
		t.Fatal("graph read required active data", continued, e)
	}
}
