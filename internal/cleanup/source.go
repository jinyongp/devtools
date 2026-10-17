package cleanup

import (
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

func readCleanupSource(kind, path string, limit int64) ([]byte, error) {
	if kind == "old_backup" {
		return platformfs.ReadExternalPrivate(path, limit)
	}
	return maintenance.Read(path, limit)
}
func restoreCleanupSource(kind, path string, body []byte) error {
	if kind == "old_backup" {
		return restoreExternalBackup(path, body)
	}
	return maintenance.Write(path, body)
}
