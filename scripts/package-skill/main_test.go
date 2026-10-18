package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestPackageSkillIncludesStandaloneResources(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "skill")
	if err := os.MkdirAll(filepath.Join(source, "references", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"SKILL.md":                   "---\nname: demo\ndescription: Demo\n---\n[Guide](references/nested/guide.md)\n",
		"references/nested/guide.md": "Complete standalone instructions.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, second := filepath.Join(root, "first.tar.gz"), filepath.Join(root, "second.tar.gz")
	for _, output := range []string{first, second} {
		if err := packageSkill(filepath.Join(source, "SKILL.md"), output); err != nil {
			t.Fatal(err)
		}
	}
	one, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatal("identical resources produced different archives")
	}
	gz, err := gzip.NewReader(bytes.NewReader(one))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	seen := map[string]string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			t.Fatalf("unexpected member type: %s", header.Name)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		seen[header.Name] = string(content)
	}
	if len(seen) != len(files) {
		t.Fatalf("got %d files; want %d", len(seen), len(files))
	}
	for name, content := range files {
		if seen["devtools/"+name] != content {
			t.Errorf("missing or changed resource: %s", name)
		}
	}
	if err := packageSkill(filepath.Join(source, "SKILL.md"), first); err == nil {
		t.Fatal("overwrote existing output")
	}
	after, err := os.ReadFile(first)
	if err != nil || !bytes.Equal(one, after) {
		t.Fatal("failed overwrite changed existing output")
	}
}

func TestPackageSkillRejectsSymlinkResource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "skill")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(source, "SKILL.md")
	if err := os.WriteFile(entry, []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(entry, filepath.Join(source, "linked.md")); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skip("Creating a symlink requires Windows developer mode or privilege")
		}
		t.Fatal(err)
	}
	output := filepath.Join(root, "output.tar.gz")
	if err := packageSkill(entry, output); err == nil {
		t.Fatal("packaged a symlink resource")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed packaging left an output archive")
	}
}
