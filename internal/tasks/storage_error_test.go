package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskDeadlineIsDistinctFromExplicitCancellation(t *testing.T) {
	store := Store{Directory: filepath.Join(privateTempDir(t), "tasks"), Profile: "deadline"}
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want, exit := "canceled", 130
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			want, exit = "deadline_exceeded", 3
		}
		cancel()
		_, err := store.Query(ctx, Query{Command: "workstream list", Options: map[string]string{}})
		if err == nil || err.Code != want || err.ExitCode != exit {
			t.Fatalf("expired=%v: %#v", expired, err)
		}
	}
}

func TestQueryCacheFailureIdentifiesCacheWithoutLeakingPath(t *testing.T) {
	root := privateTempDir(t)
	store := Store{Directory: filepath.Join(root, "tasks"), Profile: "cache-failure", Cache: filepath.Join(root, "SECRET-CACHE-PATH")}
	call(t, store, "task.add", "", Object{"title": "one"})
	call(t, store, "task.add", "", Object{"title": "two"})
	if err := os.WriteFile(store.Cache, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := store.Query(context.Background(), Query{Command: "list", Options: map[string]string{"limit": "1"}})
	if err == nil || err.Code != "storage_error" || err.Details["resource"] != "query_cache" || err.Details["operation"] == nil {
		t.Fatalf("cache failure lost: %#v", err)
	}
	body, _ := json.Marshal(err)
	if strings.Contains(string(body), "SECRET-CACHE-PATH") || strings.Contains(string(body), root) {
		t.Fatal("private path leaked")
	}
}

func TestMalformedJournalReportsValidationFailure(t *testing.T) {
	store := Store{Directory: filepath.Join(privateTempDir(t), "tasks"), Profile: "broken"}
	if err := PrivateDir(store.Directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Directory, "broken.json")
	if err := WritePrivate(path, Object{"unexpected": "SECRET-DOCUMENT"}); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.loadPath(path)
	if err == nil || err.Details["reason"] != "invalid_storage" || err.Details["os_error"] != nil {
		t.Fatalf("invalid document misclassified: %#v", err)
	}
}
