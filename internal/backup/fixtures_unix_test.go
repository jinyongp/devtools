//go:build !windows

package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureWriteFile(path string, b []byte, mode os.FileMode) error {
	return os.WriteFile(path, b, mode)
}
func fixtureMkdirAll(path string, mode os.FileMode) error { return os.MkdirAll(path, mode) }
func fixtureMkdir(path string, mode os.FileMode) error    { return os.Mkdir(path, mode) }
func privateTestRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
