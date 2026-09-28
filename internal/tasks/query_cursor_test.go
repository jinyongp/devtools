package tasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
)

func holdTaskQueryLocks(t *testing.T, s Store) func() {
	t.Helper()
	releaseGate, err := maintenance.AcquireExclusive(context.Background(), maintenance.Root(s.Directory))
	if err != nil {
		t.Fatal(err)
	}
	releaseProfile, err := LockExclusive(context.Background(), s.path()+".lock")
	if err != nil {
		releaseGate()
		t.Fatal(err)
	}
	return func() {
		releaseProfile()
		releaseGate()
	}
}

func TestPageCursorBypassesTaskStorageLocks(t *testing.T) {
	s := Store{Directory: filepath.Join(t.TempDir(), "tasks"), Profile: "cursor"}
	call(t, s, "task.add", "", Object{"title": "one"})
	call(t, s, "task.add", "", Object{"title": "two"})
	first, err := s.Query(context.Background(), Query{Command: "list", Options: map[string]string{"limit": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	cursor := str(first, "next_cursor")
	if cursor == "" {
		t.Fatal("first page did not create cursor")
	}

	release := holdTaskQueryLocks(t, s)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	next, err := s.Query(ctx, Query{Command: "list", Options: map[string]string{"limit": "1", "cursor": cursor}})
	if err != nil {
		t.Fatalf("cursor path waited on task storage locks: %v", err)
	}
	if num(next, "revision") != num(first, "revision") || len(next["items"].([]any)) != 1 || next["next_cursor"] != nil {
		t.Fatalf("cursor continuation drifted: %#v", next)
	}
}

func TestGraphCursorBypassesTaskStorageLocks(t *testing.T) {
	s := Store{Directory: filepath.Join(t.TempDir(), "tasks"), Profile: "graph-cursor"}
	a := itemID(call(t, s, "workstream.create", "", Object{"title": "A"}))
	b := itemID(call(t, s, "workstream.create", "", Object{"title": "B"}))
	c := itemID(call(t, s, "workstream.create", "", Object{"title": "C"}))
	call(t, s, "workstream.depends", b, Object{"depends_on": []string{a}})
	call(t, s, "workstream.depends", c, Object{"depends_on": []string{b}})
	first, err := s.Query(context.Background(), Query{
		Command: "workstream tree",
		Target:  c,
		Options: map[string]string{"depth": "1"},
	})
	if err != nil || first["truncated"] != true {
		t.Fatalf("first graph page: %#v %v", first, err)
	}
	cursor := str(first, "cursor")
	if cursor == "" {
		t.Fatal("graph cursor missing")
	}

	release := holdTaskQueryLocks(t, s)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	next, err := s.Query(ctx, Query{
		Command: "workstream tree",
		Target:  b,
		Options: map[string]string{"cursor": cursor},
	})
	if err != nil {
		t.Fatalf("graph cursor waited on task storage locks: %v", err)
	}
	if num(next, "revision") != num(first, "revision") {
		t.Fatalf("graph revision drift: first=%#v next=%#v", first, next)
	}
	nodes := next["nodes"].([]Object)
	if len(nodes) != 2 {
		t.Fatalf("unexpected graph continuation: %#v", next)
	}
}

func TestCursorFastPathHonorsCanceledContext(t *testing.T) {
	s := Store{Directory: filepath.Join(t.TempDir(), "tasks"), Profile: "canceled-cursor"}
	call(t, s, "task.add", "", Object{"title": "one"})
	call(t, s, "task.add", "", Object{"title": "two"})
	first, err := s.Query(context.Background(), Query{Command: "list", Options: map[string]string{"limit": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	cursor := str(first, "next_cursor")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Query(ctx, Query{Command: "list", Options: map[string]string{"limit": "1", "cursor": cursor}}); err == nil || err.Code != "canceled" || err.ExitCode != 130 {
		t.Fatalf("canceled cursor query returned %v", err)
	}
}

func TestPageCursorDoesNotReadActiveTaskStorage(t *testing.T) {
	s := Store{Directory: filepath.Join(t.TempDir(), "tasks"), Profile: "cursor-storage"}
	call(t, s, "task.add", "", Object{"title": "one"})
	call(t, s, "task.add", "", Object{"title": "two"})
	first, err := s.Query(context.Background(), Query{Command: "list", Options: map[string]string{"limit": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	cursor := str(first, "next_cursor")
	if cursor == "" {
		t.Fatal("first page did not create cursor")
	}
	resolution, ok, resolveErr := resolveV3(s.Directory, s.Profile)
	if resolveErr != nil || !ok {
		t.Fatal(resolveErr)
	}
	if err := os.WriteFile(resolution.WAL, bytes.Repeat([]byte{'x'}, 64), 0600); err != nil {
		t.Fatal(err)
	}

	next, err := s.Query(context.Background(), Query{Command: "list", Options: map[string]string{"limit": "1", "cursor": cursor}})
	if err != nil {
		t.Fatalf("cursor continuation touched active task storage: %v", err)
	}
	if num(next, "revision") != num(first, "revision") || len(next["items"].([]any)) != 1 {
		t.Fatalf("cached cursor continuation drifted: %#v", next)
	}
	if _, currentErr := s.Query(context.Background(), Query{Command: "list"}); currentErr == nil {
		t.Fatal("corrupt active storage was not actually corrupt")
	}
}
