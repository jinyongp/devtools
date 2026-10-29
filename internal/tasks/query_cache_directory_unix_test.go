//go:build linux || darwin

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitQueryCacheRejectsParentAliasesForReadAndWrite(t *testing.T) {
	s := fixture(t)
	for n := 0; n < 2; n++ {
		call(t, s, "task.add", "", Object{"title": "cache fixture"})
	}
	real := filepath.Join(privateTempDir(t), "cache-parent")
	if err := os.MkdirAll(real, 0700); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(real, "cache")
	q := Query{Command: "list", Options: map[string]string{"query-cache-dir": cache, "limit": "1"}}
	first, e := s.Query(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(privateTempDir(t), "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	q.Options["query-cache-dir"] = filepath.Join(alias, "cache")
	if _, e := s.Query(context.Background(), q); e == nil || e.Code != "storage_error" {
		t.Fatal("write followed parent alias", e)
	}
	q.Options["cursor"] = str(first, "next_cursor")
	if _, e := s.Query(context.Background(), q); e == nil || e.Code != "cursor_invalid" {
		t.Fatal("cursor followed parent alias", e)
	}
	// Current data may itself have an alias ancestor. Reject overlap before creating a leaf.
	dataAlias := filepath.Join(privateTempDir(t), "data-alias")
	if err := os.Symlink(filepath.Dir(s.Directory), dataAlias); err != nil {
		t.Fatal(err)
	}
	aliased := s
	aliased.Directory = filepath.Join(dataAlias, "tasks")
	selected, e := aliased.withQueryCache(filepath.Join(filepath.Dir(s.Directory), "hidden-cache"))
	if e != nil {
		t.Fatal("lexically separate alias should reach physical check", e)
	}
	if err := selected.prepareQueryCache(); err == nil {
		t.Fatal("cache physically inside data accepted")
	}
	if _, err := os.Stat(selected.Cache); !os.IsNotExist(err) {
		t.Fatal("overlap check created cache")
	}
	// Unknown/large metadata and symlinks are never pruned.
	name := ID() + ".json"
	if err := os.WriteFile(filepath.Join(cache, name), []byte(`{"padding":"`+strings.Repeat("x", queryCacheHeader*2)+`","expires":"2000-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cache, ID()+".json")
	if err := os.Symlink(filepath.Join(cache, name), link); err != nil {
		t.Fatal(err)
	}
	selected = s
	selected.Cache = cache
	selected.pruneQueryCaches(context.Background())
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("pruned symlink", err)
	}
	if _, err := os.Stat(filepath.Join(cache, name)); err != nil {
		t.Fatal("pruned unknown file", err)
	}
}
