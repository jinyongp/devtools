package cleanup

import (
	"context"
	"github.com/jinyongp/devtools/internal/platformfs"
	"path/filepath"
)

func restoreExternalBackup(path string, body []byte) error {
	if err := platformfs.EnsureExternalDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	return platformfs.WriteExternalPrivate(context.Background(), path, body, false)
}
