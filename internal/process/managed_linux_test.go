package process

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWSLManagedPERejectedAndLinuxExecutableAllowed(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "fixture")
	dir := t.TempDir()
	path := filepath.Join(dir, "windows-tool")
	image := make([]byte, 128)
	copy(image, "MZ")
	binary.LittleEndian.PutUint32(image[60:], 64)
	copy(image[64:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(image[68:], 0x8664)
	binary.LittleEndian.PutUint16(image[86:], 2)
	if err := os.WriteFile(path, image, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManaged([]string{path}, dir, nil); err == nil || err.Code != "unsupported_platform" {
		t.Fatalf("managed PE accepted: %v", err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManaged([]string{alias}, dir, nil); err == nil || err.Code != "unsupported_platform" {
		t.Fatalf("PE alias accepted: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManaged([]string{exe}, dir, nil); err != nil {
		t.Fatalf("Linux executable blocked: %v", err)
	}
}

func TestWSLExplicitExeLookupAndUnmodifiedEnvironment(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "fixture")
	linuxDir, windowsDir := t.TempDir(), t.TempDir()
	for _, name := range []string{"node", "pnpm", "git"} {
		native := filepath.Join(linuxDir, name)
		foreign := filepath.Join(windowsDir, name+".exe")
		if err := os.WriteFile(native, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(foreign, []byte("not executed"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, search := range []string{windowsDir + ":" + linuxDir, linuxDir} {
			selected, failure := LookPath(name, "/mnt/c/project", []string{"PATH=" + search})
			if failure != nil || selected != native {
				t.Fatal("Linux tool lookup changed", selected, failure)
			}
		}
		selected, failure := LookPath(name+".exe", "/home/user/project", []string{"PATH=" + windowsDir})
		if failure != nil || selected != foreign {
			t.Fatal("explicit exe unavailable", selected, failure)
		}
		if _, failure := LookPath(name, "/home/user/project", []string{"PATH=" + windowsDir}); failure == nil {
			t.Fatal("Windows tool satisfied Linux requirement")
		}
	}
	for _, declared := range []bool{false, true} {
		parent := []string{"PATH=" + linuxDir}
		if declared {
			parent = append(parent, "WSLENV=PUBLIC_CANARY/u")
		}
		env := Environment(parent, map[string]string{"PUBLIC_CANARY": "public-fixture", "SECRET": "never-print"})
		found := false
		for _, item := range env {
			if item == "WSLENV=PUBLIC_CANARY/u" {
				found = true
			}
		}
		if found != declared {
			t.Fatal("WSLENV opt-in changed")
		}
	}
}
