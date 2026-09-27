package archiveproof

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func writeProofEntry(t *testing.T, data, archiveID string, entry map[string]any) {
	t.Helper()
	dir := filepath.Join(data, "archives", archiveID)
	if err := tasks.PrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := tasks.WritePrivate(filepath.Join(dir, "entry.json"), entry); err != nil {
		t.Fatal(err)
	}
}

func TestCompletedProcessesRequiresExactSourcePath(t *testing.T) {
	data := t.TempDir()
	executionID := tasks.ID()
	archiveID := tasks.ID()
	expected := filepath.Join(data, "processes", executionID, "record.json")
	separator := string(os.PathSeparator)
	nonCanonical := filepath.Join(data, "processes", "other") + separator + ".." + separator + executionID + separator + "record.json"
	if filepath.Clean(nonCanonical) != expected || nonCanonical == expected {
		t.Fatalf("invalid test source: %q", nonCanonical)
	}
	writeProofEntry(t, data, archiveID, map[string]any{
		"id":          archiveID,
		"kind":        "completed_process",
		"profile":     "app",
		"source":      nonCanonical,
		"bytes":       int64(1),
		"archived_at": time.Now().UTC(),
		"restored_at": nil,
		"purged_at":   nil,
	})

	proofs, err := CompletedProcesses(data, []string{executionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 0 {
		t.Fatalf("non-canonical source became a proof: %#v", proofs)
	}
}

func TestCompletedProcessesReadsMinimumWireMetadata(t *testing.T) {
	data := t.TempDir()
	executionID := tasks.ID()
	archiveID := tasks.ID()
	archivedAt := time.Now().UTC()
	writeProofEntry(t, data, archiveID, map[string]any{
		"id":              archiveID,
		"kind":            "completed_process",
		"profile":         "app",
		"source":          filepath.Join(data, "processes", executionID, "record.json"),
		"bytes":           int64(1),
		"archived_at":     archivedAt,
		"restored_at":     nil,
		"purged_at":       nil,
		"future_metadata": map[string]any{"version": 2},
	})

	proofs, err := CompletedProcesses(data, []string{executionID})
	if err != nil {
		t.Fatal(err)
	}
	proof, ok := proofs[executionID]
	if !ok || proof.ArchiveID != archiveID || proof.Profile != "app" || !proof.ArchivedAt.Equal(archivedAt) {
		t.Fatalf("minimum metadata proof mismatch: %#v", proofs)
	}
}
