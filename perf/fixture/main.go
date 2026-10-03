// fixture creates synthetic private storage outside the timed measurements.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func main() {
	root := flag.String("root", "", "Owned fixture root")
	size := flag.Int("size", 10, "Live tasks and variable keys")
	history := flag.Int("history", 0, "Additional updates with fixed live cardinality")
	bodyKiB := flag.Int("body-kib", 1, "Task description size")
	flag.Parse()
	if err := seed(*root, *size, *history, *bodyKiB); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func seed(root string, size, history, bodyKiB int) error {
	if !filepath.IsAbs(root) || size < 1 || history < 0 || bodyKiB < 0 {
		return fmt.Errorf("absolute fixture root and nonnegative sizes required")
	}
	marker, err := os.Lstat(filepath.Join(root, ".devtools-perf"))
	if err != nil || !marker.Mode().IsRegular() {
		return fmt.Errorf("fixture ownership marker required")
	}
	data := filepath.Join(root, "data", "devtools")
	store := values.Store{Directory: filepath.Join(data, "profiles"), Profile: "perf"}
	_, valueErr := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		if _, e := state.CreateEnv("local"); e != nil {
			return false, e
		}
		for i := 0; i < size; i++ {
			if _, e := state.Set(values.Variable, fmt.Sprintf("KEY_%06d", i), "", "fixture-value"); e != nil {
				return false, e
			}
		}
		if _, e := state.Set(values.Secret, "SECRET", "", "synthetic-secret"); e != nil {
			return false, e
		}
		return true, nil
	})
	if valueErr != nil {
		return fmt.Errorf("values: %s", valueErr.Message)
	}
	j := tasks.Journal{Version: tasks.JournalVersion, Profile: "perf", Receipts: map[string]tasks.Receipt{}, Contexts: map[string]string{}}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	appendEvent := func(action, target string, data tasks.Object) {
		j.Events = append(j.Events, tasks.Event{ID: tasks.ID(), Sequence: len(j.Events) + 1, At: at, Action: action, Target: target, Data: data})
	}
	appendEvent("profile.upgraded", "", tasks.Object{"version": tasks.JournalVersion, "baseline": map[string]*tasks.DefinitionBasis{}})
	ids := make([]string, size)
	body := strings.Repeat("x", bodyKiB*1024)
	for i := range ids {
		ids[i] = tasks.ID()
		appendEvent("task.add", ids[i], tasks.Object{"title": fmt.Sprintf("Fixture task %06d", i), "description": body})
	}
	workstreamID := tasks.ID()
	appendEvent("workstream.create", workstreamID, tasks.Object{"title": "Fixture workstream"})
	appendEvent("spec.set", workstreamID, tasks.Object{"body": body, "requirements": []tasks.Object{}, "acceptance": []tasks.Object{}})
	appendEvent("plan.set", workstreamID, tasks.Object{"body": body, "task_ids": []string{}, "validation_ids": []string{}})
	// Import synthetic history in bounded batches instead of fsyncing one frame
	// per update. Live definitions retain their individual request boundaries.
	batchRequestID := tasks.ID()
	for i := 0; i < history; i++ {
		if i > 0 && i%200 == 0 {
			batchRequestID = tasks.ID()
		}
		appendEvent("task.update", ids[0], tasks.Object{"title": fmt.Sprintf("History %d", i)})
		j.Events[len(j.Events)-1].RequestID = batchRequestID
	}
	encoded, err := json.Marshal(j)
	if err != nil {
		return err
	}
	release, err := maintenance.AcquireExclusive(context.Background(), data)
	if err != nil {
		return err
	}
	defer release()
	taskStore := tasks.Store{Directory: filepath.Join(data, "tasks"), Profile: "perf"}
	staged, err := taskStore.StageExactSnapshotHeld(encoded)
	if err != nil {
		return err
	}
	if err := taskStore.PublishStagedHeld(staged); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"task_id": ids[0], "workstream_id": workstreamID, "revision": len(j.Events), "size": size, "history": history, "body_kib": bodyKiB})
}
