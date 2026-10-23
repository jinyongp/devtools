package vault

import (
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/testfs"
)

// Use a real path and the production private-directory boundary on every OS.
// Windows TempDir inherits ACLs; macOS TempDir may start with the /var alias.
func privateTempDir(t *testing.T) string {
	t.Helper()
	base := testfs.TempDir(t)
	path := filepath.Join(base, "private")
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
