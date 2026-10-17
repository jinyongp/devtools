package paths

import (
	"golang.org/x/sys/windows"
	"path/filepath"
	"testing"
)

func TestCurrentUsesKnownFolderNotEnvironment(t *testing.T) {
	t.Setenv("DEVTOOLS_ISOLATED_ROOT", "")
	t.Setenv("LOCALAPPDATA", `C:\untrusted-env-root`)
	t.Setenv("XDG_DATA_HOME", `C:\untrusted-xdg-root`)
	actual, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Data != filepath.Join(base, "devtools", "data") {
		t.Fatalf("Known Folder was not used: %q", actual.Data)
	}
}
func TestIsolatedRejectsAmbiguousWindowsRoot(t *testing.T) {
	for _, root := range []string{`C:relative`, `\\server\share\private`, `C:\private:stream`, `\\?\C:\private`} {
		if _, err := Isolated(root); err == nil {
			t.Fatalf("unsafe isolated root accepted: %q", root)
		}
	}
}
