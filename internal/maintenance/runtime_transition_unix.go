//go:build !windows

package maintenance

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func stageRuntimeDirectory(_ context.Context, _ *platformfs.Root, root, from, to string) error {
	if err := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); err != nil {
		return err
	}
	if err := syncDir(filepath.Join(root, filepath.Dir(to))); err != nil {
		return err
	}
	return syncDir(root)
}
