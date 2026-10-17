package tasks

import (
	"github.com/jinyongp/devtools/internal/platformfs"
	"os/exec"
	"path/filepath"
	"testing"
)

func privateTempDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private")
	r, e := platformfs.OpenRoot(path, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Close(); e != nil {
		t.Fatal(e)
	}
	return path
}
func directoryAlias(actual, alias string) error {
	return exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, actual).Run()
}
