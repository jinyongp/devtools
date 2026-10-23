//go:build !windows

package profilekey

import (
	"os"
	"testing"

	"github.com/jinyongp/devtools/internal/testfs"
)

func privateTempDir(t *testing.T) string {
	t.Helper()
	dir := testfs.TempDir(t)
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func directoryAlias(actual, alias string) error { return os.Symlink(actual, alias) }
