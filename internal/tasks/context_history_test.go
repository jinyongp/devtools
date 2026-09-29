package tasks

import (
	"context"
	"fmt"
	"testing"
)

func writeLegacyHistorylessSnapshot(t *testing.T, path string, snapshot materializedState) {
	t.Helper()
	snapshot.HistoryEvents = nil
	snapshot.HistoryRefs = nil
	snapshot.Checksum = materializedStateChecksum(snapshot)
	legacy := struct {
		FormatVersion int                         `json:"format_version"`
		Profile       string                      `json:"profile"`
		Version       int                         `json:"version"`
		Revision      int                         `json:"revision"`
		WALOffset     int64                       `json:"wal_offset"`
		Items         map[string]*Item            `json:"items"`
		ItemOrders    map[string]int              `json:"item_orders"`
		Runs          map[string]*Run             `json:"runs"`
		Tracking      map[string]*DefinitionBasis `json:"tracking"`
		LastEventAt   string                      `json:"last_event_at,omitempty"`
		Checksum      string                      `json:"checksum"`
	}{
		FormatVersion: snapshot.FormatVersion,
		Profile:       snapshot.Profile,
		Version:       snapshot.Version,
		Revision:      snapshot.Revision,
		WALOffset:     snapshot.WALOffset,
		Items:         snapshot.Items,
		ItemOrders:    snapshot.ItemOrders,
		Runs:          snapshot.Runs,
		Tracking:      snapshot.Tracking,
		LastEventAt:   snapshot.LastEventAt,
		Checksum:      snapshot.Checksum,
	}
	if err := WritePrivate(path, legacy); err != nil {
		t.Fatal(err)
	}
}

func TestContextUsesSnapshotRecentHistory(t *testing.T) {
	store, taskID, revision, _ := benchmarkV3HistoryStore(t, 300)
	state, err := store.loadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Events) != 0 {
		t.Fatalf("current loader replayed checkpointed history: %d events", len(state.Events))
	}
	history := state.recentHistory(map[string]bool{taskID: true})
	if len(history) != contextHistoryLimit || history[0].Sequence != revision-contextHistoryLimit+1 || history[len(history)-1].Sequence != revision {
		t.Fatalf("recent history mismatch: first=%d last=%d len=%d", history[0].Sequence, history[len(history)-1].Sequence, len(history))
	}
	result, queryErr := store.Query(context.Background(), Query{Command: "context", Target: taskID})
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	contextHistory, ok := result["history"].([]Event)
	if !ok || len(contextHistory) != contextHistoryLimit || contextHistory[0].Sequence != revision-contextHistoryLimit+1 || contextHistory[len(contextHistory)-1].Sequence != revision {
		t.Fatalf("context history mismatch: %#v", result["history"])
	}
}

func TestMissingSnapshotRecentHistoryFallsBackAndRepairs(t *testing.T) {
	store, taskID, revision, _ := benchmarkV3HistoryStore(t, 40)
	storage, resolveErr := store.resolveStorage()
	if resolveErr != nil || storage.V3 == nil {
		t.Fatalf("resolve storage: %#v %+v", storage, resolveErr)
	}
	var snapshot materializedState
	if err := ReadPrivate(storage.V3.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	writeLegacyHistorylessSnapshot(t, storage.V3.Snapshot, snapshot)

	current, currentErr := store.loadCurrent()
	if currentErr != nil || current.Revision != revision || len(current.Events) != 0 || current.historyComplete {
		t.Fatalf("legacy snapshot current read regressed: revision=%d events=%d complete=%v err=%v", current.Revision, len(current.Events), current.historyComplete, currentErr)
	}
	contextResult, queryErr := store.Query(context.Background(), Query{Command: "context", Target: taskID})
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	contextHistory, ok := contextResult["history"].([]Event)
	if !ok || len(contextHistory) != contextHistoryLimit || contextHistory[len(contextHistory)-1].Sequence != revision {
		t.Fatalf("context did not fall back to WAL history: %#v", contextResult["history"])
	}
	legacy, legacyState, legacyErr := readMaterializedSnapshot(storage.V3.Snapshot, store.Profile)
	if legacyErr != nil || legacy.HistoryEvents != nil || legacy.HistoryRefs != nil || legacyState.historyComplete {
		t.Fatalf("read-only context rewrote legacy snapshot: %#v %#v %v", legacy, legacyState, legacyErr)
	}

	result, execErr := store.Execute(context.Background(), Request{
		Action: "task.update",
		Target: taskID,
		Body:   Object{"description": "repair"},
		Options: map[string]string{
			"request-id":  ID(),
			"if-revision": fmt.Sprint(revision),
		},
	})
	if execErr != nil {
		t.Fatal(execErr)
	}
	if num(result, "revision") != revision+1 {
		t.Fatalf("unexpected revision: %#v", result)
	}
	repaired, _, err := readMaterializedSnapshot(storage.V3.Snapshot, store.Profile)
	if err != nil || repaired.HistoryEvents == nil || repaired.HistoryRefs == nil {
		t.Fatalf("snapshot recent history was not repaired: %#v %v", repaired, err)
	}
}

func TestInvalidSnapshotRecentHistoryFallsBackToWAL(t *testing.T) {
	store, _, revision, _ := benchmarkV3HistoryStore(t, 40)
	storage, resolveErr := store.resolveStorage()
	if resolveErr != nil || storage.V3 == nil {
		t.Fatalf("resolve storage: %#v %+v", storage, resolveErr)
	}
	var snapshot materializedState
	if err := ReadPrivate(storage.V3.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.HistoryRefs["not-an-event-target"] = []int{revision}
	snapshot.Checksum = materializedStateChecksum(snapshot)
	if err := WritePrivate(storage.V3.Snapshot, snapshot); err != nil {
		t.Fatal(err)
	}
	current, currentErr := store.loadCurrent()
	if currentErr != nil || current.Revision != revision || len(current.Events) != revision {
		t.Fatalf("invalid accelerator did not fall back to WAL: revision=%d events=%d err=%v", current.Revision, len(current.Events), currentErr)
	}
}

func BenchmarkTaskContextRecentHistory(b *testing.B) {
	for _, historyEvents := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("history_%d", historyEvents), func(b *testing.B) {
			store, taskID, _, historyBytes := benchmarkV3HistoryStore(b, historyEvents)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if _, err := store.Query(context.Background(), Query{Command: "context", Target: taskID}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(historyBytes)/(1<<20), "history-MiB")
		})
	}
}
