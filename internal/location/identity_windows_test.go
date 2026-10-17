package location

import (
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

func TestWindowsExistingIdentityPreservesCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MixedCase")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if !Same(path, strings.ToLower(path)) {
		t.Fatal("existing directory spelling changed its identity")
	}
	if !Same(filepath.Join(path, "Missing"), filepath.Join(strings.ToLower(path), "missing")) {
		t.Fatal("case-insensitive missing suffix did not share ancestor identity")
	}
}
func TestWindowsMissingSuffixRespectsDirectoryCaseFlag(t *testing.T) {
	path := t.TempDir()
	ptr, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(ptr, windows.FILE_WRITE_ATTRIBUTES|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	var flag uint32 = windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR
	err = windows.SetFileInformationByHandle(h, windows.FileCaseSensitiveInfo, (*byte)(unsafe.Pointer(&flag)), 4)
	windows.CloseHandle(h)
	if err != nil {
		if os.Getenv("CI") == "true" {
			t.Fatalf("case-sensitive directory fixture: %v", err)
		}
		t.Skipf("local case-sensitive directory flag unavailable: %v", err)
	}
	if Same(filepath.Join(path, "Missing"), filepath.Join(path, "missing")) {
		t.Fatal("case-sensitive suffix was folded")
	}
	if !Same(filepath.Join(path, "Missing"), filepath.Join(path, "Missing")) {
		t.Fatal("identical missing suffix lost identity")
	}
}
func TestWindowsLocationRejectsDriveRelativeAndNamespace(t *testing.T) {
	for _, path := range []string{`C:relative`, `\\server\share\project`, `\\?\C:\project`, `C:\project:stream`} {
		if _, err := Canonical(path); err == nil {
			t.Fatalf("ambiguous project path accepted: %q", path)
		}
	}
}

func createDirectoryAlias(actual, alias string) error {
	return exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, actual).Run()
}
