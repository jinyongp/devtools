package cleanup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	profilecatalog "github.com/jinyongp/devtools/internal/profiles"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestPreviewConflictArchiveRestoreAndPurge(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	ctx := context.Background()
	path := filepath.Join(e.Cache, "task-queries", tasks.ID()+".json")
	b, _ := json.Marshal(map[string]any{"expires": time.Now().Add(-time.Hour), "items": []any{}})
	if er := maintenance.Write(path, b); er != nil {
		t.Fatal(er)
	}
	plan, err := e.Preview(ctx, "")
	if err != nil || len(plan.Items) != 1 {
		t.Fatal(plan, err)
	}
	other := append(append([]byte{}, b...), byte(' '))
	maintenance.Write(path, other)
	if _, err = e.Apply(ctx, plan.ID, []string{plan.Items[0].ID}, tasks.ID()); err == nil || err.Code != "revision_conflict" {
		t.Fatal(err)
	}
	plan, err = e.Preview(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	id := plan.Items[0].ID
	request := tasks.ID()
	result, err := e.Apply(ctx, plan.ID, []string{id}, request)
	if err != nil || len(result.Archives) != 1 {
		t.Fatal(result, err)
	}
	if _, er := os.Stat(path); !os.IsNotExist(er) {
		t.Fatal("source retained", er)
	}
	result, err = e.Apply(ctx, plan.ID, []string{id}, request)
	if err != nil || !result.Replayed {
		t.Fatal(result, err)
	}
	if _, changed, err := e.Purge(ctx, id); err == nil || err.Code != "retention_active" || changed {
		t.Fatal(changed, err)
	}
	if _, changed, err := e.Restore(ctx, id); err != nil || !changed {
		t.Fatal(changed, err)
	}
	restored, er := os.ReadFile(path)
	if er != nil || string(restored) != string(other) {
		t.Fatal("restore mismatch", er)
	}
	if _, changed, err := e.Restore(ctx, id); err != nil || changed {
		t.Fatal(changed, err)
	}
	var a Archive
	tasks.ReadPrivate(e.archivePath(id, "entry.json"), &a)
	a.ArchivedAt = time.Now().Add(-31 * 24 * time.Hour)
	write(e.archivePath(id, "entry.json"), a)
	if _, changed, err := e.Purge(ctx, id); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if _, changed, err := e.Purge(ctx, id); err != nil || changed {
		t.Fatal(changed, err)
	}
	if _, changed, err := e.Restore(ctx, id); err == nil || err.Code != "archive_purged" || changed {
		t.Fatal(changed, err)
	}
}
func TestActiveTaskProfileRetained(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")}
	s := tasks.Store{Directory: filepath.Join(e.Data, "tasks"), Profile: "test"}
	_, err := s.Execute(context.Background(), tasks.Request{Action: "task.add", Options: map[string]string{"request-id": tasks.ID()}, Body: tasks.Object{"title": "active"}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.Preview(context.Background(), "test")
	if err != nil || len(plan.Items) != 0 {
		t.Fatal(plan, err)
	}
}

func TestErrorExitCodes(t *testing.T) {
	engine := Engine{Data: t.TempDir(), Cache: t.TempDir()}
	if _, err := engine.Apply(context.Background(), "bad-id", []string{"bad-id"}, "bad-id"); err == nil || err.Code != "invalid_argument" || err.ExitCode != 2 {
		t.Fatal("invalid cleanup input contract", err)
	}
	if _, _, err := engine.Restore(context.Background(), tasks.ID()); err == nil || err.Code != "archive_not_found" || err.ExitCode != 3 {
		t.Fatal("missing archive contract", err)
	}
	for code, want := range map[string]int{"invalid_argument": 2, "revision_conflict": 3, "storage_error": 1} {
		if err := fail(code); err.ExitCode != want {
			t.Errorf("%s exit code = %d, want %d", code, err.ExitCode, want)
		}
	}
}

func TestOldBackupRestoreSurvivesBackupDirectoryChange(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	oldDirectory := filepath.Join(root, "old-backups")
	if err := write(filepath.Join(e.Config, "backup.json"), map[string]any{"directory": oldDirectory}); err != nil {
		t.Fatal(err)
	}
	var oldest string
	for i := 0; i < 4; i++ {
		path := filepath.Join(oldDirectory, "devtools-"+tasks.ID()+".age")
		if err := maintenance.Write(path, []byte("encrypted-backup")); err != nil {
			t.Fatal(err)
		}
		age := time.Now().Add(-time.Duration(40-i) * 24 * time.Hour)
		if err := os.Chtimes(path, age, age); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			oldest = path
		}
	}
	ctx := context.Background()
	plan, failure := e.Preview(ctx, "")
	if failure != nil || len(plan.Items) != 1 || plan.Items[0].Source != oldest {
		t.Fatal(plan, failure)
	}
	id := plan.Items[0].ID
	if _, failure := e.Apply(ctx, plan.ID, []string{id}, tasks.ID()); failure != nil {
		t.Fatal(failure)
	}
	if err := write(filepath.Join(e.Config, "backup.json"), map[string]any{"directory": filepath.Join(root, "new-backups")}); err != nil {
		t.Fatal(err)
	}
	if _, changed, failure := e.Restore(ctx, id); failure != nil || !changed {
		t.Fatal(changed, failure)
	}
	if body, err := os.ReadFile(oldest); err != nil || string(body) != "encrypted-backup" {
		t.Fatal(string(body), err)
	}
	if _, changed, failure := e.Restore(ctx, id); failure != nil || changed {
		t.Fatal(changed, failure)
	}
}

func TestCompletedProcessCleanupPreservesProfileEnumeration(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	id := tasks.ID()
	ended := time.Now().UTC().Add(-31 * 24 * time.Hour)
	record := services.Record{
		ID:        id,
		Profile:   "archived",
		Directory: filepath.Join(root, "project"),
		Command:   "web",
		CreatedAt: ended.Add(-time.Hour),
		EndedAt:   &ended,
		State:     "stopped",
		Capture:   true,
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Write(filepath.Join(engine.Data, "processes", id, "record.json"), body); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(engine.Data, "processes", id, "output.log")
	if err := maintenance.Write(logPath, []byte("legacy-log")); err != nil {
		t.Fatal(err)
	}

	plan, failure := engine.Preview(context.Background(), "archived")
	if failure != nil {
		t.Fatal(failure)
	}
	var processItem Item
	for _, item := range plan.Items {
		if item.Kind == "completed_process" {
			processItem = item
			break
		}
	}
	if processItem.ID == "" {
		t.Fatalf("completed process candidate missing: %#v", plan.Items)
	}
	if _, failure := engine.Apply(context.Background(), plan.ID, []string{processItem.ID}, tasks.ID()); failure != nil {
		t.Fatal(failure)
	}
	if _, err := os.Stat(filepath.Join(engine.Data, "processes", id, "record.json")); !os.IsNotExist(err) {
		t.Fatalf("record was not retired: %v", err)
	}

	if items, failure := (services.Store{Data: engine.Data}).List(context.Background(), "archived"); failure != nil || len(items) != 0 {
		t.Fatalf("process list failed after legacy-style cleanup: %#v %v", items, failure)
	}
	names, failure := (profilecatalog.Catalog{Data: engine.Data}).Names(context.Background())
	if failure != nil {
		t.Fatalf("profile enumeration failed after process cleanup: %v", failure)
	}
	if len(names) != 0 {
		t.Fatalf("archived process leaked into active profile names: %#v", names)
	}
	if _, err := os.Stat(filepath.Join(engine.Data, "processes", id, "record.archive.json")); err != nil {
		t.Fatalf("archive marker missing: %v", err)
	}

	residual, failure := engine.Preview(context.Background(), "archived")
	if failure != nil {
		t.Fatal(failure)
	}
	foundLog := false
	for _, item := range residual.Items {
		if item.Kind == "expired_log" && item.Source == logPath {
			foundLog = true
		}
	}
	if !foundLog {
		t.Fatalf("residual log was not rediscovered: %#v", residual.Items)
	}

	if _, changed, failure := engine.Restore(context.Background(), processItem.ID); failure != nil || !changed {
		t.Fatalf("process restore failed: changed=%v err=%v", changed, failure)
	}
	if _, err := os.Stat(filepath.Join(engine.Data, "processes", id, "record.json")); err != nil {
		t.Fatalf("record was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(engine.Data, "processes", id, "record.archive.json")); !os.IsNotExist(err) {
		t.Fatalf("archive marker remained after restore: %v", err)
	}
}

func TestApplyResumesDurableWorksetAfterProcessRetirement(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	ctx := context.Background()
	executionID := tasks.ID()
	ended := time.Now().UTC().Add(-31 * 24 * time.Hour)
	record := services.Record{
		ID:        executionID,
		Profile:   "resume",
		Directory: filepath.Join(root, "project"),
		Command:   "web",
		CreatedAt: ended.Add(-time.Hour),
		EndedAt:   &ended,
		State:     "stopped",
		Capture:   true,
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(engine.Data, "processes", executionID, "record.json")
	logPath := filepath.Join(engine.Data, "processes", executionID, "output.log")
	if err := maintenance.Write(recordPath, body); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Write(logPath, []byte("resume-log")); err != nil {
		t.Fatal(err)
	}

	plan, failure := engine.Preview(ctx, "resume")
	if failure != nil {
		t.Fatal(failure)
	}
	var stored snapshot
	planPath := filepath.Join(engine.Cache, "cleanup", plan.ID+".json")
	if err := tasks.ReadPrivate(planPath, &stored); err != nil {
		t.Fatal(err)
	}
	var processCandidate, logCandidate candidate
	for _, item := range stored.Items {
		switch item.Kind {
		case "completed_process":
			processCandidate = item
		case "expired_log":
			logCandidate = item
		}
	}
	if processCandidate.ID == "" || logCandidate.ID == "" {
		t.Fatalf("expected process+log workset: %#v", stored.Items)
	}
	ids := []string{processCandidate.ID, logCandidate.ID}
	sort.Strings(ids)
	selected := []candidate{processCandidate, logCandidate}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Kind+":"+selected[i].Source < selected[j].Kind+":"+selected[j].Source
	})
	requestID := tasks.ID()
	receiptPath := filepath.Join(engine.Data, "cleanup-receipts", requestID+".json")
	if err := write(receiptPath, receipt{Plan: plan.ID, IDs: ids, Items: selected, Result: Result{Archives: []Archive{}}}); err != nil {
		t.Fatal(err)
	}

	processArchive, retireErr := engine.retire(ctx, processCandidate)
	if retireErr != nil {
		t.Fatal(retireErr)
	}
	if _, err := os.Stat(recordPath); !os.IsNotExist(err) {
		t.Fatalf("process record survived simulated first mutation: %v", err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log disappeared before resume: %v", err)
	}
	if err := os.Remove(planPath); err != nil {
		t.Fatal(err)
	}

	result, failure := engine.Apply(ctx, plan.ID, ids, requestID)
	if failure != nil {
		t.Fatal(failure)
	}
	if !result.Replayed || len(result.Archives) != 2 {
		t.Fatalf("resume did not converge: %#v", result)
	}
	foundProcess := false
	foundLog := false
	for _, archive := range result.Archives {
		if archive.ID == processArchive.ID {
			foundProcess = true
		}
		if archive.ID == logCandidate.ID {
			foundLog = true
		}
	}
	if !foundProcess || !foundLog {
		t.Fatalf("resume archives mismatch: %#v", result.Archives)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("residual log survived resume: %v", err)
	}
	var completed receipt
	if err := tasks.ReadPrivate(receiptPath, &completed); err != nil || !completed.Done || len(completed.Items) != 2 {
		t.Fatalf("receipt did not complete: %#v %v", completed, err)
	}
}

func TestLegacyIncompleteReceiptIgnoresExpiredPreviewTTL(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	ctx := context.Background()
	source := filepath.Join(engine.Cache, "task-queries", tasks.ID()+".json")
	body, _ := json.Marshal(map[string]any{"expires": time.Now().Add(-time.Hour), "items": []any{}})
	if err := maintenance.Write(source, body); err != nil {
		t.Fatal(err)
	}
	plan, failure := engine.Preview(ctx, "")
	if failure != nil || len(plan.Items) != 1 {
		t.Fatalf("preview failed: %#v %v", plan, failure)
	}
	var stored snapshot
	planPath := filepath.Join(engine.Cache, "cleanup", plan.ID+".json")
	if err := tasks.ReadPrivate(planPath, &stored); err != nil {
		t.Fatal(err)
	}
	stored.Expires = time.Now().Add(-time.Hour)
	if err := tasks.WritePrivate(planPath, stored); err != nil {
		t.Fatal(err)
	}
	requestID := tasks.ID()
	ids := []string{plan.Items[0].ID}
	if err := write(filepath.Join(engine.Data, "cleanup-receipts", requestID+".json"), receipt{Plan: plan.ID, IDs: ids}); err != nil {
		t.Fatal(err)
	}

	result, failure := engine.Apply(ctx, plan.ID, ids, requestID)
	if failure != nil || !result.Replayed || len(result.Archives) != 1 {
		t.Fatalf("legacy receipt did not resume past preview TTL: %#v %v", result, failure)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("legacy receipt source retained: %v", err)
	}
}
func TestLegacyArchivedProcessResidualLogIsCleanupCandidate(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	executionID := tasks.ID()
	processDir := filepath.Join(engine.Data, "processes", executionID)
	if err := tasks.PrivateDir(processDir); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(processDir, "output.log")
	if err := maintenance.Write(logPath, []byte("legacy-residual-log")); err != nil {
		t.Fatal(err)
	}
	archiveID := tasks.ID()
	archivedAt := time.Now().UTC().Add(-31 * 24 * time.Hour)
	purgedAt := archivedAt.Add(30 * 24 * time.Hour)
	legacy := Archive{
		Item: Item{
			ID:      archiveID,
			Kind:    "completed_process",
			Profile: "legacy",
			Source:  filepath.Join(processDir, "record.json"),
			Bytes:   128,
		},
		ArchivedAt: archivedAt,
		PurgedAt:   &purgedAt,
	}
	if err := write(engine.archivePath(archiveID, "entry.json"), legacy); err != nil {
		t.Fatal(err)
	}

	plan, failure := engine.Preview(context.Background(), "legacy")
	if failure != nil {
		t.Fatal(failure)
	}
	var logItem Item
	for _, item := range plan.Items {
		if item.Kind == "expired_log" && item.Source == logPath {
			logItem = item
			break
		}
	}
	if logItem.ID == "" {
		t.Fatalf("legacy residual log missing from preview: %#v", plan.Items)
	}
	result, failure := engine.Apply(context.Background(), plan.ID, []string{logItem.ID}, tasks.ID())
	if failure != nil || len(result.Archives) != 1 {
		t.Fatalf("legacy residual log cleanup failed: %#v %v", result, failure)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("legacy residual log was not removed: %v", err)
	}
}

func TestLegacyIncompleteReceiptWithoutSnapshotConflicts(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	ctx := context.Background()
	source := filepath.Join(engine.Cache, "task-queries", tasks.ID()+".json")
	body, _ := json.Marshal(map[string]any{"expires": time.Now().Add(-time.Hour), "items": []any{}})
	if err := maintenance.Write(source, body); err != nil {
		t.Fatal(err)
	}
	plan, failure := engine.Preview(ctx, "")
	if failure != nil || len(plan.Items) != 1 {
		t.Fatalf("preview failed: %#v %v", plan, failure)
	}
	requestID := tasks.ID()
	ids := []string{plan.Items[0].ID}
	if err := write(filepath.Join(engine.Data, "cleanup-receipts", requestID+".json"), receipt{Plan: plan.ID, IDs: ids}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(engine.Cache, "cleanup", plan.ID+".json")); err != nil {
		t.Fatal(err)
	}

	if _, failure := engine.Apply(ctx, plan.ID, ids, requestID); failure == nil || failure.Code != "revision_conflict" {
		t.Fatalf("missing legacy snapshot was guessed: %v", failure)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source changed after blocked legacy resume: %v", err)
	}
}

func TestExpiredLogCleanupUsesLogicalTailAcrossCompactionAndRestore(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	ctx := context.Background()
	executionID := tasks.ID()
	ended := time.Now().UTC().Add(-8 * 24 * time.Hour)
	record := services.Record{
		ID:        executionID,
		Profile:   "logs",
		Instance:  "instance",
		Directory: filepath.Join(root, "project"),
		Command:   "web",
		CreatedAt: ended.Add(-time.Hour),
		EndedAt:   &ended,
		State:     "stopped",
		Capture:   true,
	}
	recordBody, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(engine.Data, "processes", executionID, "record.json")
	logPath := filepath.Join(engine.Data, "processes", executionID, "output.log")
	if err := maintenance.Write(recordPath, recordBody); err != nil {
		t.Fatal(err)
	}
	logical := bytes.Repeat([]byte("l"), 1<<20)
	physical := append(bytes.Repeat([]byte("p"), 200<<10), logical...)
	if len(physical) >= 1280<<10 {
		t.Fatalf("invalid physical fixture size: %d", len(physical))
	}
	if err := maintenance.Write(logPath, physical); err != nil {
		t.Fatal(err)
	}

	plan, failure := engine.Preview(ctx, "logs")
	if failure != nil {
		t.Fatal(failure)
	}
	var logItem Item
	for _, item := range plan.Items {
		if item.Kind == "expired_log" {
			logItem = item
			break
		}
	}
	if logItem.ID == "" || logItem.Bytes != int64(len(logical)) {
		t.Fatalf("logical log candidate mismatch: %#v", plan.Items)
	}

	// Simulate background compaction changing only the physical representation.
	if err := maintenance.Write(logPath, logical); err != nil {
		t.Fatal(err)
	}
	result, failure := engine.Apply(ctx, plan.ID, []string{logItem.ID}, tasks.ID())
	if failure != nil || len(result.Archives) != 1 {
		t.Fatalf("logical-tail apply failed: %#v %v", result, failure)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("retired log survived apply: %v", err)
	}
	payload, err := maintenance.Read(engine.archivePath(logItem.ID, "payload"), 2<<20)
	if err != nil || !bytes.Equal(payload, logical) {
		t.Fatalf("archive payload drifted: len=%d err=%v", len(payload), err)
	}

	if _, changed, failure := engine.Restore(ctx, logItem.ID); failure != nil || !changed {
		t.Fatalf("log restore failed: changed=%v err=%v", changed, failure)
	}
	restored, exists, logErr := (services.Store{Data: engine.Data}).LogSnapshot(ctx, executionID)
	if logErr != nil || !exists || !bytes.Equal(restored, logical) {
		t.Fatalf("restored logical log mismatch: exists=%v len=%d err=%v", exists, len(restored), logErr)
	}
}

func TestLegacyCompletedTaskArchiveRestoresIntoV3Storage(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	store := tasks.Store{Directory: filepath.Join(engine.Data, "tasks"), Profile: "legacy-task"}
	created, err := store.Execute(context.Background(), tasks.Request{
		Action:  "task.add",
		Body:    tasks.Object{"title": "Legacy archived task"},
		Options: map[string]string{"request-id": tasks.ID()},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["item"].(tasks.Object)["id"].(string)

	release, lockErr := maintenance.AcquireExclusive(context.Background(), engine.Data)
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	snapshot, snapshotErr := store.ExportSnapshotHeld(128 << 20)
	if snapshotErr != nil {
		release()
		t.Fatal(snapshotErr)
	}
	files, deleteErr := store.DeletionFilesHeld()
	if deleteErr != nil {
		release()
		t.Fatal(deleteErr)
	}
	if err := maintenance.Replace(engine.Data, files); err != nil {
		release()
		t.Fatal(err)
	}
	_ = store.CollectOrphanGenerationsHeld()
	release()

	archiveID := tasks.ID()
	archive := Archive{
		Item: Item{
			ID:      archiveID,
			Kind:    "completed_tasks",
			Profile: "legacy-task",
			Source:  profilekey.LegacyPath(filepath.Join(engine.Data, "tasks"), "legacy-task"),
			Bytes:   int64(len(snapshot.Data)),
		},
		ArchivedAt: time.Now().UTC().Add(-time.Hour),
	}
	if err := maintenance.Write(engine.archivePath(archiveID, "payload"), snapshot.Data); err != nil {
		t.Fatal(err)
	}
	if err := write(engine.archivePath(archiveID, "entry.json"), archive); err != nil {
		t.Fatal(err)
	}

	restored, changed, restoreErr := engine.Restore(context.Background(), archiveID)
	if restoreErr != nil || !changed {
		t.Fatalf("legacy task restore: changed=%v archive=%#v err=%v", changed, restored, restoreErr)
	}
	if restored.Source != archive.Source {
		t.Fatalf("archive audit source changed: got %q want %q", restored.Source, archive.Source)
	}
	state, readErr := store.Read(context.Background())
	if readErr != nil || state.Items[taskID] == nil {
		t.Fatalf("restored task state missing: %#v %v", state, readErr)
	}
	resolved, resolveErr := profilekey.Resolve(store.Directory, store.Profile, "tasks")
	if resolveErr != nil || resolved.Mode != profilekey.ModeCanonical {
		t.Fatalf("legacy archive did not publish canonical task storage: %#v %v", resolved, resolveErr)
	}
	marker, markerErr := maintenance.Read(profilekey.CanonicalPath(store.Directory, store.Profile), 4096)
	if markerErr != nil || !bytes.Contains(marker, []byte("\"storage_marker\":\"task-v3\"")) {
		t.Fatalf("legacy archive did not publish v3 task marker: %q %v", marker, markerErr)
	}
}
