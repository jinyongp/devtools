package main

import (
	"context"
	"os"
	"path/filepath"
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
		if err := seed(root, 3, history, 64); err != nil {
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
	if err := seed(root, 3, 0, 1); err == nil {
		t.Fatal("accepted unowned fixture")
	}
	if _, err := os.Stat(filepath.Join(root, "data")); !os.IsNotExist(err) {
		t.Fatal("wrote before ownership check")
	}
}
