//go:build !windows

package services

import (
	"os"
	"testing"

	"github.com/jinyongp/devtools/internal/testfs"
)

func privateTempDir(t testing.TB) string {
	t.Helper()
	dir := testfs.TempDir(t)
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
