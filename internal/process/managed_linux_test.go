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
