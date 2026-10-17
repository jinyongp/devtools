package profiles

import (
	"context"
	"github.com/jinyongp/devtools/internal/platformfs"
	"path/filepath"
	"testing"
)

func privateTempDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private")
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func writeProfileFixture(path string, body []byte) error {
	root, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(context.Background(), filepath.Base(path), body, false)
}
