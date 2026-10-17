package services

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

func logPrivacy(path string, _ os.FileInfo) bool {
	f, e := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
	if e != nil {
		return false
	}
	defer f.Close()
	return platformfs.CheckFile(f.File) == nil
}
