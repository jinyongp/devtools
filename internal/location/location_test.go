package location

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalResolvesSymlinkAndMissingSuffix(t *testing.T) {
	base := t.TempDir()
	actual := filepath.Join(base, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}

	canonicalActual, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Canonical(alias); err != nil || got != canonicalActual {
		t.Fatalf("canonical alias = %q, %v; want %q", got, err, canonicalActual)
	}
	wantMissing := filepath.Join(canonicalActual, "missing", "child")
	if got, err := Canonical(filepath.Join(alias, "missing", "child")); err != nil || got != wantMissing {
		t.Fatalf("canonical missing suffix = %q, %v; want %q", got, err, wantMissing)
	}
	if !Same(alias, canonicalActual) || !Same(filepath.Join(alias, "missing"), filepath.Join(canonicalActual, "missing")) {
		t.Fatal("alias and canonical paths did not share identity")
	}
	if Same("", canonicalActual) {
		t.Fatal("empty stored location was reinterpreted as the current directory")
	}
}

func TestExistingDirectoryRejectsFileAndMissingPath(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExistingDirectory(file); err == nil {
		t.Fatal("file accepted as directory")
	}
	if _, err := ExistingDirectory(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
}
