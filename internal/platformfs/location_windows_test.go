package platformfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsEntryLocationObservesWithoutCreating(t *testing.T) {
	root := testRoot(t)
	if err := root.MkdirAll("nested"); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(context.Background(), `nested\state`, []byte("preserved"), false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nested", `nested\state`} {
		if different, err := root.EntryPathAliased(name); err != nil || different {
			t.Fatalf("ordinary entry %s: alias=%v err=%v", name, different, err)
		}
	}
	if _, err := root.EntryPathAliased(`missing\state`); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing entry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root.path, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("observer created a directory: %v", err)
	}
	if body, err := root.ReadFile(`nested\state`, 100); err != nil || string(body) != "preserved" {
		t.Fatalf("observer changed data: %q %v", body, err)
	}
	for _, name := range []string{".", `..\outside`} {
		if _, err := root.EntryPathAliased(name); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("invalid entry %s accepted: %v", name, err)
		}
	}
}

func TestWindowsEntryLocationRejectsReparse(t *testing.T) {
	root, outside := testRoot(t), testRoot(t)
	alias := filepath.Join(root.path, "alias")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, outside.path).CombinedOutput(); err != nil {
		t.Fatalf("junction fixture: %v %s", err, output)
	}
	defer os.Remove(alias)
	for _, name := range []string{"alias", `alias\state`} {
		if _, err := root.EntryPathAliased(name); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("reparse entry %s accepted: %v", name, err)
		}
	}
}

func TestWindowsEntryLocationRejectsUnsafeEntries(t *testing.T) {
	root := testRoot(t)
	if err := os.Mkdir(filepath.Join(root.path, "inherited"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := root.EntryPathAliased("inherited"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("inherited ACL accepted: %v", err)
	}
	if err := root.WriteFile(context.Background(), "state", []byte("original"), false); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root.path, "hardlink")
	if err := os.Link(filepath.Join(root.path, "state"), alias); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(alias)
	if _, err := root.EntryPathAliased("state"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("hard link accepted: %v", err)
	}
}

func TestWindowsEntryLocationRecognizesRedirectedNames(t *testing.T) {
	// Redacted replay of observed names. File identity is checked separately;
	// this comparison describes a name difference even for unpackaged tokens.
	root := `\Users\fixture\AppData\Local\devtools\data`
	for _, tc := range []struct {
		actual string
		want   bool
	}{
		{root + `\.maintenance\lock.turnstile`, false},
		{`\USERS\FIXTURE\APPDATA\LOCAL\DEVTOOLS\DATA\.MAINTENANCE\LOCK.TURNSTILE`, false},
		{`\Users\fixture\AppData\Local\Packages\fixture\LocalCache\Local\devtools\data\.maintenance\lock.turnstile`, true},
	} {
		if got := entryPathAliased(root, `.maintenance\lock.turnstile`, tc.actual); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.actual, got, tc.want)
		}
	}
}
