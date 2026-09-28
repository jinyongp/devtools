package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
)

func benchmarkV3HistoryStore(tb testing.TB, historyEvents int) (Store, string, int, int64) {
	tb.Helper()
	if historyEvents < 2 {
		historyEvents = 2
	}
	root := tb.TempDir()
	store := Store{Directory: filepath.Join(root, "tasks"), Profile: "bench"}
	if err := PrivateDir(store.Directory); err != nil {
		tb.Fatal(err)
	}
	resolution, err := createGeneration(store.Directory, store.Profile, ID())
	if err != nil {
		tb.Fatal(err)
	}
	state := NewState()
	taskID := ID()
	at := stamp()
	events := make([]Event, 0, historyEvents)
	events = append(events, Event{
		ID:       "upgrade",
		Sequence: 1,
		At:       at,
		Action:   "profile.upgraded",
		Data: Object{
			"version":  JournalVersion,
			"baseline": map[string]*DefinitionBasis{},
		},
	})
	events = append(events, Event{
		ID:       "add",
		Sequence: 2,
		At:       at,
		Action:   "task.add",
		Target:   taskID,
		Data:     Object{"title": "bench", "description": "a"},
	})
	for sequence := 3; sequence <= historyEvents; sequence++ {
		description := "a"
		if sequence%2 == 0 {
			description = "b"
		}
		events = append(events, Event{
			ID:       fmt.Sprintf("e-%d", sequence),
			Sequence: sequence,
			At:       at,
			Action:   "task.update",
			Target:   taskID,
			Data:     Object{"description": description},
		})
	}
	for _, event := range events {
		state.Apply(event)
	}
	frame := walFrame{
		Meta: walFrameMeta{
			Kind:             "historical",
			PreviousRevision: 0,
			FinalRevision:    historyEvents,
			EventCount:       len(events),
		},
		Events: events,
	}
	temp := frameTempPath(resolution)
	if _, _, err := writeFrameFile(temp, frame); err != nil {
		tb.Fatal(err)
	}
	if _, err := appendFrameFile(resolution.WAL, temp); err != nil {
		_ = os.Remove(temp)
		tb.Fatal(err)
	}
	_ = os.Remove(temp)
	info, err := os.Stat(resolution.WAL)
	if err != nil {
		tb.Fatal(err)
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState(store.Profile, state, info.Size(), at)); err != nil {
		tb.Fatal(err)
	}
	if err := generationDurable(resolution); err != nil {
		tb.Fatal(err)
	}
	storage, resolveErr := store.resolveStorage()
	if resolveErr != nil {
		tb.Fatal(resolveErr)
	}
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		tb.Fatal(err)
	}
	if err := store.publishV3(storage, resolution); err != nil {
		release()
		tb.Fatal(err)
	}
	release()
	return store, taskID, state.Revision, info.Size()
}

func BenchmarkTaskStorageCurrentList(b *testing.B) {
	for _, history := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("history_%d", history), func(b *testing.B) {
			store, _, _, historyBytes := benchmarkV3HistoryStore(b, history)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := store.Query(context.Background(), Query{Command: "list", Options: map[string]string{"scope": "all"}}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(historyBytes)/(1<<20), "history-MiB")
		})
	}
}

func BenchmarkTaskStorageMutation(b *testing.B) {
	for _, history := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("history_%d", history), func(b *testing.B) {
			store, taskID, revision, historyBytes := benchmarkV3HistoryStore(b, history)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				description := "bench-a"
				if i%2 == 0 {
					description = "bench-b"
				}
				result, err := store.Execute(context.Background(), Request{
					Action: "task.update",
					Target: taskID,
					Body:   Object{"description": description},
					Options: map[string]string{
						"request-id":  ID(),
						"if-revision": strconv.Itoa(revision),
					},
				})
				if err != nil {
					b.Fatal(err)
				}
				revision = num(result, "revision")
			}
			b.ReportMetric(float64(historyBytes)/(1<<20), "history-MiB")
		})
	}
}

func BenchmarkTaskStorageCursorSecondPage(b *testing.B) {
	store, _, _, historyBytes := benchmarkV3HistoryStore(b, 100_000)
	if _, err := store.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "second"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		b.Fatal(err)
	}
	first, err := store.Query(context.Background(), Query{Command: "list", Options: map[string]string{"scope": "all", "limit": "1"}})
	if err != nil {
		b.Fatal(err)
	}
	cursor := str(first, "next_cursor")
	if cursor == "" {
		b.Fatal("cursor not created")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Query(context.Background(), Query{Command: "list", Options: map[string]string{"scope": "all", "limit": "1", "cursor": cursor}}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(historyBytes)/(1<<20), "history-MiB")
}
