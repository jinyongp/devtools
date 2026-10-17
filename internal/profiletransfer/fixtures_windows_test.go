package profiletransfer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func fixtureWriteFile(path string, b []byte, mode os.FileMode) error {
	f, err := platformfs.OpenExternalPrivate(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return errors.Join(err, f.Close())
}
func fixtureMkdirAll(path string, _ os.FileMode) error {
	r, err := platformfs.OpenRoot(path, true)
	if err != nil {
		return err
	}
	return r.Close()
}
func fixtureMkdir(path string, mode os.FileMode) error {
	if _, err := os.Lstat(path); err == nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrExist}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fixtureMkdirAll(path, mode)
}
func privateTestRoot(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private")
	if err := fixtureMkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertIdentityPrivacy(t *testing.T, s Store) {
	t.Helper()
	if err := platformfs.CheckDirectory(s.directory()); err != nil {
		t.Fatal(err)
	}
	f, err := platformfs.OpenPrivate(s.identityPath(), os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = platformfs.CheckFile(f.File); err != nil {
		t.Fatal(err)
	}
}
