package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
)

func benchmarkV3HistoryStore(tb testing.TB, historyEvents int) (Store, string, int, int64) {
	tb.Helper()
	if historyEvents < 2 {
		historyEvents = 2
	}
	root := filepath.Join(tb.TempDir(), "data")
	if err := PrivateDir(root); err != nil {
		tb.Fatal(err)
	}
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

func benchmarkActiveWorkstreamState(bodyBytes int) (*State, string) {
	state := NewState()
	state.Version = JournalVersion
	body := strings.Repeat("x", bodyBytes)
	counts := []int{13, 38, 7, 10, 6, 9, 54, 21, 27}
	order := 1
	firstTask := ""
	for workstreamIndex, count := range counts {
		workstreamID := fmt.Sprintf("workstream-%02d", workstreamIndex)
		requirements := []Object{}
		acceptance := []Object{}
		for taskIndex := 0; taskIndex < count; taskIndex++ {
			requirementKey := fmt.Sprintf("R-%02d-%03d", workstreamIndex, taskIndex)
			acceptanceKey := fmt.Sprintf("A-%02d-%03d", workstreamIndex, taskIndex)
			requirements = append(requirements, Object{"key": requirementKey, "text": "requirement"})
			acceptance = append(acceptance, Object{"key": acceptanceKey, "text": "acceptance", "requirement_keys": []string{requirementKey}})
		}
		workstream := &Item{
			ID: workstreamID, Kind: "workstream", State: "active", Title: workstreamID, Description: "benchmark",
			Props: Object{
				"spec": Object{"body": body, "requirements": requirements, "acceptance": acceptance},
				"plan": Object{"body": body},
			},
			Revision: order, Created: stamp(), Updated: stamp(), Order: order,
		}
		state.Items[workstreamID] = workstream
		workstreamBasis := newDefinitionBasis(order)
		order++
		for taskIndex := 0; taskIndex < count; taskIndex++ {
			taskID := fmt.Sprintf("task-%02d-%03d", workstreamIndex, taskIndex)
			if firstTask == "" {
				firstTask = taskID
			}
			requirementKey := fmt.Sprintf("R-%02d-%03d", workstreamIndex, taskIndex)
			acceptanceKey := fmt.Sprintf("A-%02d-%03d", workstreamIndex, taskIndex)
			task := &Item{
				ID: taskID, Kind: "task", State: "open", Title: taskID, Description: "benchmark",
				Workstream: workstreamID, Props: Object{"acceptance_keys": []string{acceptanceKey}},
				Revision: order, Created: stamp(), Updated: stamp(), Order: order,
			}
			state.Items[taskID] = task
			state.Tracking[taskID] = newDefinitionBasis(order)
			workstreamBasis.Order = append(workstreamBasis.Order, taskID)
			workstreamBasis.KeyEpochs["acceptance:"+acceptanceKey] = workstream.Revision
			workstreamBasis.KeyEpochs["requirement:"+requirementKey] = workstream.Revision
			order++
		}
		state.Tracking[workstreamID] = workstreamBasis
	}
	state.Revision = order
	return state, firstTask
}

func benchmarkV3WorkstreamTailStore(tb testing.TB, bodyBytes, tailFrames int) Store {
	tb.Helper()
	root := filepath.Join(tb.TempDir(), "data")
	if err := PrivateDir(root); err != nil {
		tb.Fatal(err)
	}
	store := Store{Directory: filepath.Join(root, "tasks"), Profile: "bench-workstreams"}
	if err := PrivateDir(store.Directory); err != nil {
		tb.Fatal(err)
	}
	resolution, err := createGeneration(store.Directory, store.Profile, ID())
	if err != nil {
		tb.Fatal(err)
	}
	state, target := benchmarkActiveWorkstreamState(bodyBytes)
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState(store.Profile, state, 0, stamp())); err != nil {
		tb.Fatal(err)
	}
	previousRevision := state.Revision
	for index := 0; index < tailFrames; index++ {
		event := Event{
			ID:       fmt.Sprintf("tail-%03d", index),
			Sequence: previousRevision + 1,
			At:       stamp(),
			Action:   "task.update",
			Target:   target,
			Data:     Object{"description": fmt.Sprintf("tail-%03d", index)},
		}
		frame := walFrame{
			Meta: walFrameMeta{
				Kind:             "historical",
				PreviousRevision: previousRevision,
				FinalRevision:    previousRevision + 1,
				EventCount:       1,
			},
			Events: []Event{event},
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
		previousRevision++
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
	return store
}

func BenchmarkTaskStorageWorkstreamListTail(b *testing.B) {
	for _, tailFrames := range []int{128, 255} {
		b.Run(fmt.Sprintf("tail_%d", tailFrames), func(b *testing.B) {
			store := benchmarkV3WorkstreamTailStore(b, 256<<10, tailFrames)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				result, err := store.Query(context.Background(), Query{Command: "workstream list", Options: map[string]string{"state": "all", "limit": "200"}})
				if err != nil {
					b.Fatal(err)
				}
				if len(result["items"].([]any)) != 9 {
					b.Fatal("unexpected workstream count")
				}
			}
			b.ReportMetric(float64(tailFrames), "tail-frames")
			b.ReportMetric(256, "body-KiB")
		})
	}
}

func BenchmarkTaskStorageWorkstreamContextTail(b *testing.B) {
	for _, tailFrames := range []int{128, 255} {
		b.Run(fmt.Sprintf("tail_%d", tailFrames), func(b *testing.B) {
			store := benchmarkV3WorkstreamTailStore(b, 256<<10, tailFrames)
			state, err := store.ReadCurrent(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			target := state.List("workstream")[0].ID
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				result, err := store.Query(context.Background(), Query{Command: "workstream context", Target: target})
				if err != nil {
					b.Fatal(err)
				}
				if result["truncated"] != false || len(result["tasks"].([]any)) != 13 {
					b.Fatal("unexpected context scope")
				}
			}
			b.ReportMetric(float64(tailFrames), "tail-frames")
			b.ReportMetric(256, "body-KiB")
		})
	}
}

func benchmarkItemPatch(item *Item, basis *DefinitionBasis) Object {
	return Object{
		"id": item.ID, "kind": item.Kind, "title": item.Title, "description": item.Description,
		"workstream_id": item.Workstream, "depends_on": item.Depends, "props": copyObject(item.Props),
		"basis": basis, "order": item.Order,
	}
}

func benchmarkV3LifecycleTailStore(tb testing.TB, bodyBytes, tailFrames int) (Store, v3Resolution) {
	tb.Helper()
	root := filepath.Join(tb.TempDir(), "data")
	if err := PrivateDir(root); err != nil {
		tb.Fatal(err)
	}
	store := Store{Directory: filepath.Join(root, "tasks"), Profile: "bench-lifecycle"}
	if err := PrivateDir(store.Directory); err != nil {
		tb.Fatal(err)
	}
	resolution, err := createGeneration(store.Directory, store.Profile, ID())
	if err != nil {
		tb.Fatal(err)
	}

	blueprint, target := benchmarkActiveWorkstreamState(bodyBytes)
	state := NewState()
	upgrade := Event{
		ID: ID(), Sequence: 1, At: stamp(), Action: "profile.upgraded",
		Data: Object{"version": JournalVersion, "baseline": map[string]*DefinitionBasis{}},
	}
	if err := applyReplayEvent(state, upgrade, ""); err != nil {
		tb.Fatal(err)
	}
	if _, err := appendPreparedFrame(resolution, walFrame{
		Meta:   walFrameMeta{Kind: "historical", PreviousRevision: 0, FinalRevision: 1, EventCount: 1},
		Events: []Event{upgrade},
	}, nil, nil); err != nil {
		tb.Fatal(err)
	}

	for _, workstream := range blueprint.List("workstream") {
		patches := []Object{benchmarkItemPatch(workstream, blueprint.Tracking[workstream.ID])}
		for _, task := range blueprint.List("task") {
			if task.Workstream == workstream.ID {
				patches = append(patches, benchmarkItemPatch(task, blueprint.Tracking[task.ID]))
			}
		}
		event := Event{
			ID: ID(), Sequence: state.Revision + 1, At: stamp(), Action: "workstream.edited", Target: workstream.ID,
			Data: Object{"patches": patches, "affected_ids": []string{}},
		}
		if err := applyReplayEvent(state, event, ""); err != nil {
			tb.Fatal(err)
		}
		if _, err := appendPreparedFrame(resolution, walFrame{
			Meta: walFrameMeta{
				Kind: "historical", PreviousRevision: event.Sequence - 1,
				FinalRevision: event.Sequence, EventCount: 1,
			},
			Events: []Event{event},
		}, nil, nil); err != nil {
			tb.Fatal(err)
		}
	}

	info, err := os.Stat(resolution.WAL)
	if err != nil {
		tb.Fatal(err)
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState(store.Profile, state, info.Size(), stamp())); err != nil {
		tb.Fatal(err)
	}
	signature := state.Assessment(target).Signature
	previousRevision := state.Revision
	for index := 0; index < tailFrames; index++ {
		requestID := ID()
		fingerprint := fmt.Sprintf("lifecycle-%03d", index)
		runID := ID()
		event := Event{
			ID: ID(), Sequence: previousRevision + 1, At: stamp(), RequestID: requestID,
			Action: "run.claimed", Target: target,
			Data: Object{"run_id": runID, "directory": "/tmp"},
		}
		receipt := newReceiptCoordination(requestID, fingerprint, "", Object{
			"item": Object{"definition_signature": signature},
			"run":  Object{"definition_signature": signature},
		}, "")
		ref := receiptReference(receipt)
		frame := walFrame{
			Meta: walFrameMeta{
				Kind: "mutation", PreviousRevision: previousRevision, FinalRevision: previousRevision + 1,
				RequestID: requestID, Fingerprint: fingerprint, EventCount: 1, HasReceipt: true,
			},
			Events: []Event{event}, Receipt: &ref,
		}
		if _, err := appendPreparedFrame(resolution, frame, &receipt, nil); err != nil {
			tb.Fatal(err)
		}
		previousRevision++
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
	return store, resolution
}

func BenchmarkTaskStorageLifecycleReplay(b *testing.B) {
	for _, tailFrames := range []int{32, 128, 255} {
		b.Run(fmt.Sprintf("current_tail_%d", tailFrames), func(b *testing.B) {
			store, _ := benchmarkV3LifecycleTailStore(b, 100<<10, tailFrames)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				if _, err := store.Query(context.Background(), Query{
					Command: "workstream list", Options: map[string]string{"state": "all", "limit": "200"},
				}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(tailFrames), "tail-frames")
			b.ReportMetric(100, "body-KiB")
		})
		b.Run(fmt.Sprintf("history_tail_%d", tailFrames), func(b *testing.B) {
			_, resolution := benchmarkV3LifecycleTailStore(b, 100<<10, tailFrames)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				state, err := loadHistoryV3(resolution)
				if err != nil {
					b.Fatal(err)
				}
				if state.Revision == 0 {
					b.Fatal("history replay returned empty state")
				}
			}
			b.ReportMetric(float64(tailFrames), "tail-frames")
			b.ReportMetric(100, "body-KiB")
		})
	}
}

func benchmarkV3LargeExportHistoryStore(tb testing.TB, frames, payloadBytes int) Store {
	tb.Helper()
	store, resolution := benchmarkV3LifecycleTailStore(tb, 4<<10, 0)
	state, err := store.ReadCurrent(context.Background())
	if err != nil {
		tb.Fatal(err)
	}
	padding := strings.Repeat("x", payloadBytes)
	previousRevision := state.Revision
	for index := 0; index < frames; index++ {
		requestID := ID()
		fingerprint := fmt.Sprintf("large-export-%06d", index)
		event := Event{
			ID: ID(), Sequence: previousRevision + 1, At: stamp(), RequestID: requestID,
			Action: "workstream.edited", Target: "workstream-00",
			Data: Object{"patches": []Object{}, "reason": padding},
		}
		receipt := newReceiptCoordination(requestID, fingerprint, "", Object{
			"changed":           true,
			"revision":          previousRevision + 1,
			"previous_revision": previousRevision,
			"action_ids":        []string{event.ID},
			"affected_ids":      []string{},
			"affected_count":    0,
		}, "")
		ref := receiptReference(receipt)
		frame := walFrame{
			Meta: walFrameMeta{
				Kind: "mutation", PreviousRevision: previousRevision, FinalRevision: previousRevision + 1,
				RequestID: requestID, Fingerprint: fingerprint, EventCount: 1, HasReceipt: true,
			},
			Events: []Event{event}, Receipt: &ref,
		}
		if _, err := appendPreparedFrame(resolution, frame, &receipt, nil); err != nil {
			tb.Fatal(err)
		}
		previousRevision++
	}
	if err := generationDurable(resolution); err != nil {
		tb.Fatal(err)
	}
	current, replayErr := loadCurrentFromWAL(resolution)
	if replayErr != nil {
		tb.Fatal(replayErr)
	}
	walInfo, statErr := os.Stat(resolution.WAL)
	if statErr != nil {
		tb.Fatal(statErr)
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState(store.Profile, current, walInfo.Size(), stamp())); err != nil {
		tb.Fatal(err)
	}
	return store
}

func BenchmarkTaskStorageLargeHistoryExport(b *testing.B) {
	for _, test := range []struct {
		frames       int
		payloadBytes int
	}{
		{frames: 256, payloadBytes: 64 << 10},
		{frames: 1024, payloadBytes: 64 << 10},
		{frames: 1500, payloadBytes: 64 << 10},
	} {
		b.Run(fmt.Sprintf("frames_%d_payload_%dKiB", test.frames, test.payloadBytes>>10), func(b *testing.B) {
			store := benchmarkV3LargeExportHistoryStore(b, test.frames, test.payloadBytes)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				snapshot, err := store.ExportSnapshotHeld(128 << 20)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(snapshot.Data))/(1024*1024), "snapshot-MiB")
			}
		})
	}
}
