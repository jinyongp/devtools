//go:build !windows

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHomebrewUpdateUsesTheNewFormulaExecutableForResume(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"opt/bin", "old", "new"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(root, "old", "devtools")
	if err := os.WriteFile(old, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "opt", "bin", "devtools")
	if err := os.Symlink(old, link); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"brew": `#!/bin/sh
set -eu
cd "$(dirname "$0")"
case "$1:$2" in
  --prefix:jinyongp/tap/devtools) printf '%s\n' "$PWD/opt" ;;
  upgrade:jinyongp/tap/devtools) ln -sf "$PWD/new/devtools" opt/bin/devtools ;;
  *) exit 2 ;;
esac
`,
		"new/devtools": `#!/bin/sh
case "$*" in
  '--format json version') printf '%s\n' '{"ok":true,"data":{"version":"0.1.0"}}' ;;
  '__install-storage') printf '%s\n' '{"status":"ready","ready":true,"report":{"scope":"storage","ready":true}}' ;;
  *) exit 2 ;;
esac
`,
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(root, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	expected, err := filepath.EvalSymlinks(old)
	if err != nil {
		t.Fatal(err)
	}
	result, failure := updateHomebrew(context.Background(), expected)
	if failure != nil {
		t.Fatal(failure)
	}
	data := result.(map[string]any)
	if data["version"] != "0.1.0" || data["binary_changed"] != true {
		t.Fatal(data)
	}
}
