package testfixture

import (
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
	"runtime"
	"testing"
)

// AssertPrivate checks the native security boundary and retains exact Unix modes.
func AssertPrivate(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	var err error
	if mode.IsDir() {
		err = platformfs.CheckDirectory(path)
	} else {
		var f *platformfs.File
		f, err = platformfs.OpenExternalPrivate(path, os.O_RDONLY, 0)
		if err == nil {
			err = platformfs.CheckFile(f.File)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		t.Fatal("private object", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != mode.Perm() {
		t.Fatalf("private mode %s: %v", path, info.Mode())
	}
}
