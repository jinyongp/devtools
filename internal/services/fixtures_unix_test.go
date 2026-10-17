//go:build !windows

package services

import (
	"os"
	"testing"
)

func fixtureWriteFile(path string, b []byte, mode os.FileMode) error {
	return os.WriteFile(path, b, mode)
}
func fixtureMkdirAll(path string, mode os.FileMode) error { return os.MkdirAll(path, mode) }
func fixtureMkdir(path string, mode os.FileMode) error    { return os.Mkdir(path, mode) }
func privateTestRoot(t testing.TB) string                 { t.Helper(); return t.TempDir() }

func logPrivacy(_ string, i os.FileInfo) bool { return i.Mode().Perm() == 0600 }
