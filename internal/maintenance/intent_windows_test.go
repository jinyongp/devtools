package maintenance

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func privateTempDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private")
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func waitForWriterIntent(t *testing.T, path string) {
	t.Helper()
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(ptr, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	deadline := time.Now().Add(time.Second)
	for {
		err = windows.LockFileEx(handle, windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = windows.UnlockFileEx(handle, 0, 1, 0, &windows.Overlapped{}); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("writer never acquired turnstile")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func directoryAlias(actual, alias string) error {
	return exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, actual).Run()
}
