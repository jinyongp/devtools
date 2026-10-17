//go:build !windows

package profiletransfer

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

func assertIdentityPrivacy(t *testing.T, s Store) {
	t.Helper()
	d, e := os.Lstat(s.directory())
	if e != nil || d.Mode().Perm() != 0700 {
		t.Fatalf("directory mode: %v %v", d, e)
	}
	f, e := os.Lstat(s.identityPath())
	if e != nil || f.Mode().Perm() != 0600 {
		t.Fatalf("identity mode: %v %v", f, e)
	}
}
