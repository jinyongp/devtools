//go:build !windows

package values

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func assertPrivateStoredPath(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	want := os.FileMode(0600)
	if info.IsDir() {
		want = 0700
	}
	if info.Mode().Perm() != want {
		t.Fatal("private mode mismatch")
	}
}
func assertUnsafeStoreRejected(t *testing.T, store Store) {
	if err := os.Chmod(store.file(), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil || err.Code != "storage_error" {
		t.Fatal(err)
	}
	if err := os.Remove(store.file()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(privateTempDir(t), "target")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, store.file()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil {
		t.Fatal("followed storage symlink")
	}
}
func holdProfileLock(t *testing.T, path string) func() {
	t.Helper()
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }
}
