package maintenance

import (
	"context"
	"github.com/jinyongp/devtools/internal/platformfs"
	"path/filepath"
)

func Write(path string, data []byte) error {
	root, err := platformfs.OpenRoot(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(context.Background(), filepath.Base(path), data, true)
}
