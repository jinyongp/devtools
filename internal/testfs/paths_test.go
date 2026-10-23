package testfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFixtureAliasesSelectOnePhysicalRoot(t *testing.T) {
	root := TempDir(t)
	physical := filepath.Join(root, "physical")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if runtime.GOOS == "windows" {
		if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, physical).CombinedOutput(); err != nil {
			t.Fatalf("junction fixture: %v: %s", err, output)
		}
	} else if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	got := CanonicalDir(t, alias)
	if got != physical {
		t.Fatalf("fixture alias selected %q, want %q", got, physical)
	}
}
