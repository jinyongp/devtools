//go:build !windows

package maintenance

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
