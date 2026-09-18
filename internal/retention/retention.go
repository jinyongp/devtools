// Package retention centralizes fixed local-data retention policy.
package retention

import "time"

const (
	RawProcessLogAge    = 7 * 24 * time.Hour
	CleanupCandidateAge = 30 * 24 * time.Hour
	ArchivePurgeAge     = 30 * 24 * time.Hour
	QuerySnapshotTTL    = 30 * time.Minute
	CleanupPreviewTTL   = 10 * time.Minute
	MinimumBackupCopies = 3
)
