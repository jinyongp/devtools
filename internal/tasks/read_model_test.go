package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadIndexDoesNotSurviveMutationOrPreview(t *testing.T) {
	s := NewState()
	s.Apply(Event{Action: "task.add", Target: "a", Sequence: 1, Data: Object{"title": "a"}})
	s.Apply(Event{Action: "task.add", Target: "b", Sequence: 2, Data: Object{"title": "b", "workstream_id": "w"}})
	s.Items["b"].Depends = []string{"a"}
	s.prepareReads()
	if !reflect.DeepEqual(s.successors("a"), []string{"b"}) || len(s.workstreamTasks("w")) != 1 {
		t.Fatal("missing reverse relation or membership")
	}
	list := s.List("task")
	list[0] = nil
	if s.List("task")[0] == nil {
		t.Fatal("caller changed index order")
	}
	preview := s.clone()
	preview.Items["b"].Depends = nil
	if preview.readIndex != nil || len(preview.successors("a")) != 0 || len(s.successors("a")) != 1 {
		t.Fatal("preview reused or changed source index")
	}
	for n, run := range []string{"old", "new"} {
		s.Apply(Event{Action: "run.claimed", Target: "a", Sequence: n + 3, Data: Object{"run_id": run}})
		if s.readIndex != nil {
			t.Fatal("event retained read index")
		}
		s.prepareReads()
		if s.Current("a").ID != run {
			t.Fatal("active run did not advance")
		}
	}
	s.Apply(Event{Action: "run.released", Target: "a", Sequence: 5, Data: Object{"run_id": "old"}})
	s.prepareReads()
	if s.Current("a").ID != "new" {
		t.Fatal("ending an old run removed the new run")
	}
}

func TestGraphSnapshotFreezesViewsAndDoesNotNeedJournal(t *testing.T) {
	s := fixture(t)
	call(t, s, "task.add", "", Object{"title": "first"})
	b := itemID(call(t, s, "task.add", "", Object{"title": "frozen"}))
	run := call(t, s, "run.claimed", b, Object{})
	first, err := s.Query(context.Background(), Query{Command: "tree", Options: map[string]string{"limit": "1"}})
	if err != nil || str(first, "cursor") == "" {
		t.Fatal(first, err)
	}
	token := str(first, "cursor")
	body, readErr := os.ReadFile(filepath.Join(s.cacheDirectory(), token+".json"))
	if readErr != nil || strings.Contains(string(body), `"events"`) || strings.Contains(string(body), str(run, "context")) {
		t.Fatal("snapshot retained history or credentials", readErr)
	}
	call(t, s, "run.released", runID(run), Object{"summary": "released"}, "context", str(run, "context"))
	call(t, s, "task.update", b, Object{"title": "changed"})
	// Even an unavailable live journal cannot affect a cursor's frozen view.
	cache := s.cacheDirectory()
	s.Directory = filepath.Join(t.TempDir(), "unavailable")
	s.Cache = cache
	next, err := s.Query(context.Background(), Query{Command: "tree", Target: b, Options: map[string]string{"cursor": token, "depth": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	nodes := next["nodes"].([]Object)
	if len(nodes) != 1 || str(nodes[0], "title") != "frozen" || nodes[0]["running"] != true || num(next, "revision") != num(first, "revision") {
		t.Fatal("cursor used current state", next)
	}
}

func TestGraphSnapshotViewsDetachMutableValues(t *testing.T) {
	s := NewState()
	s.Apply(Event{Action: "task.add", Target: "a", Sequence: 1, Data: Object{"title": "a", "custom": Object{"nested": []any{"old"}}}})
	snapshot := s.graphSnapshot("test", "task")
	s.Items["a"].Props["custom"].(Object)["nested"].([]any)[0] = "new"
	encoded, err := json.Marshal(snapshot.Nodes["a"])
	if err != nil || !strings.Contains(string(encoded), `"old"`) || strings.Contains(string(encoded), `"new"`) {
		t.Fatal("frozen view aliases live properties", string(encoded), err)
	}
}
