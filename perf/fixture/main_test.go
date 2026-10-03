package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestSeedPreservesCardinalityAndBodiesAcrossHistory(t *testing.T) {
	for _, history := range []int{0, 401} {
		root := t.TempDir()
		if err := os.Chmod(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".devtools-perf"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := seed(root, 3, history, 64, 0, false); err != nil {
			t.Fatal(err)
		}
		data := filepath.Join(root, "data", "devtools")
		state, err := (tasks.Store{Directory: filepath.Join(data, "tasks"), Profile: "perf"}).Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if state.Revision != 7+history || len(state.Items) != 4 {
			t.Fatalf("revision=%d items=%d", state.Revision, len(state.Items))
		}
		for _, item := range state.Items {
			if item.Kind == "task" && len(item.Description) != 64*1024 {
				t.Fatal("history changed task body size")
			}
		}
		valueState, valueErr := (values.Store{Directory: filepath.Join(data, "profiles"), Profile: "perf"}).Read(context.Background())
		if valueErr != nil {
			t.Fatal(valueErr)
		}
		if len(valueState.Keys) != 4 || !valueState.Envs["local"] {
			t.Fatal("unexpected variable/secret/environment cardinality")
		}
	}
}

func TestSeedRejectsUnownedRoot(t *testing.T) {
	root := t.TempDir()
	if err := seed(root, 3, 0, 1, 0, false); err == nil {
		t.Fatal("accepted unowned fixture")
	}
	if _, err := os.Stat(filepath.Join(root, "data")); !os.IsNotExist(err) {
		t.Fatal("wrote before ownership check")
	}
}

func TestSeedRetainsCompletedWorkstreamsWithFixedLiveCardinality(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devtools-perf"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := seed(root, 3, 0, 4, 5, true); err != nil {
		t.Fatal(err)
	}
	store := tasks.Store{Directory: filepath.Join(root, "data", "devtools", "tasks"), Profile: "perf"}
	state, err := store.ReadCurrent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.List("task")) != 3 || len(state.List("workstream")) != 6 {
		t.Fatal("fixture cardinality changed")
	}
	for _, task := range state.List("task") {
		owner := state.Items[task.Workstream]
		if owner == nil || owner.Kind != "workstream" || owner.State != "draft" {
			t.Fatal("live task not attached to selected workstream")
		}
	}
	var selected *tasks.Item
	for _, item := range state.List("workstream") {
		if item.State == "draft" {
			selected = item
		}
	}
	plan := selected.Props["plan"].(map[string]any)
	if len(plan["task_ids"].([]any)) != 3 {
		t.Fatal("plan omitted attached tasks")
	}
	_, mutationErr := store.Execute(context.Background(), tasks.Request{Action: "plan.set", Target: selected.ID,
		Body:    tasks.Object{"body": "Updated fixture document", "task_ids": plan["task_ids"], "validation_ids": []string{}},
		Options: map[string]string{"request-id": tasks.ID(), "if-revision": strconv.Itoa(state.Revision)}})
	if mutationErr != nil {
		t.Fatal("attached fixture cannot measure plan updates", mutationErr)
	}
	completed := 0
	documentBodies := map[string]bool{}
	for _, item := range state.List("workstream") {
		if item.State == "done" {
			completed++
		}
		for _, name := range []string{"spec", "plan"} {
			doc := item.Props[name].(map[string]any)
			if len(doc["body"].(string)) != 4*1024 {
				t.Fatal("missing retained body")
			}
			body := doc["body"].(string)
			if documentBodies[body] {
				t.Fatal("fixture repeats document bodies across documents")
			}
			documentBodies[body] = true
		}
	}
	if completed != 5 {
		t.Fatal("completed workstreams not retained")
	}
}
