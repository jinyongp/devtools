package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
)

func TestPublishStagedKeepsCommittedSuccessWhenOrphanCleanupFails(t *testing.T) {
	root := t.TempDir()
	source := Store{Directory: filepath.Join(root, "source", "tasks"), Profile: "source"}
	if _, err := source.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "Archived task"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}

	release, err := maintenance.AcquireExclusive(context.Background(), maintenance.Root(source.Directory))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ExportSnapshotHeld(128 << 20)
	release()
	if err != nil || snapshot.Data == nil {
		t.Fatalf("source snapshot: %v %#v", err, snapshot)
	}

	targetRoot := filepath.Join(root, "target")
	target := Store{Directory: filepath.Join(targetRoot, "tasks"), Profile: "target"}
	targetSnapshot, restoreErr := RestoreSnapshot(snapshot.Data, source.Profile, target.Profile)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	release, err = maintenance.AcquireExclusive(context.Background(), targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := target.StageExactSnapshotHeld(targetSnapshot)
	if err != nil {
		release()
		t.Fatal(err)
	}
	generationRoot := taskGenerationRoot(target.Directory, target.Profile)
	if err := os.Mkdir(filepath.Join(generationRoot, "not-a-generation"), 0700); err != nil {
		release()
		t.Fatal(err)
	}
	if err := target.PublishStagedHeld(staged); err != nil {
		release()
		t.Fatalf("post-commit orphan failure escaped publication: %v", err)
	}
	release()

	// The logical publication committed even though orphan cleanup was blocked.
	readState, readErr := target.Read(context.Background())
	if readErr != nil || len(readState.Items) != 1 {
		t.Fatalf("committed target unreadable: %#v %v", readState, readErr)
	}

	// New mutations fail closed until orphan cleanup can complete.
	if _, err := target.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "Blocked"},
		Options: map[string]string{"request-id": ID()},
	}); err == nil || err.Code != "storage_error" {
		t.Fatalf("orphan cleanup failure did not block next mutation: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(generationRoot, "not-a-generation")); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "After cleanup"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatalf("mutation did not recover after orphan cleanup: %v", err)
	}
}
