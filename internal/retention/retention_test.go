package retention

import (
	"testing"
	"time"
)

func TestDefaultPolicyRemainsStable(t *testing.T) {
	if RawProcessLogAge != 7*24*time.Hour {
		t.Fatalf("raw log age = %s", RawProcessLogAge)
	}
	if CleanupCandidateAge != 30*24*time.Hour || ArchivePurgeAge != 30*24*time.Hour {
		t.Fatalf("30-day policy drifted: candidate=%s archive=%s", CleanupCandidateAge, ArchivePurgeAge)
	}
	if QuerySnapshotTTL != 30*time.Minute || CleanupPreviewTTL != 10*time.Minute {
		t.Fatalf("TTL policy drifted: query=%s preview=%s", QuerySnapshotTTL, CleanupPreviewTTL)
	}
	if MinimumBackupCopies != 3 {
		t.Fatalf("minimum backup copies = %d", MinimumBackupCopies)
	}
}
