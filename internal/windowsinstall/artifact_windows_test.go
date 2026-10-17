package windowsinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsInstallArtifactRejectsUnsafeArchivesAndChecksums(t *testing.T) {
	version := "0.0.0-test.1"
	arch, err := NativeArchitecture()
	if err != nil {
		t.Fatal(err)
	}
	name := "devtools_" + version + "_windows_" + arch + ".zip"
	for _, kind := range []string{"traversal", "duplicate", "symlink", "directory", "checksum", "checksum_name"} {
		t.Run(kind, func(t *testing.T) {
			source := t.TempDir()
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			header := zip.FileHeader{Name: "devtools.exe", Method: zip.Deflate}
			header.SetMode(0700)
			switch kind {
			case "traversal":
				header.Name = "../devtools.exe"
			case "symlink":
				header.SetMode(os.ModeSymlink | 0700)
			case "directory":
				header.Name = "devtools.exe/"
				header.SetMode(os.ModeDir | 0700)
			}
			entry, err := writer.CreateHeader(&header)
			if err != nil {
				t.Fatal(err)
			}
			entry.Write([]byte("fixture"))
			if kind == "duplicate" {
				entry, err = writer.CreateHeader(&header)
				if err != nil {
					t.Fatal(err)
				}
				entry.Write([]byte("fixture"))
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			sum := checksum(archive.Bytes())
			sumName := name
			if kind == "checksum" {
				sum = strings.Repeat("0", 64)
			}
			if kind == "checksum_name" {
				sumName = "different.zip"
			}
			if err = os.WriteFile(filepath.Join(source, name), archive.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(source, name+".sha256"), []byte(sum+"  "+sumName+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err = release(context.Background(), version, source, arch); err == nil {
				t.Fatal("unsafe release artifact accepted")
			}
		})
	}
}
func TestWindowsInstallRealPackageIdentityAndWrongCPU(t *testing.T) {
	path := os.Getenv("DEVTOOLS_INSTALL_TEST_ZIP")
	if path == "" {
		// Direct developer go test also gets a real package image; Actions reuses its ZIP.
		directory := t.TempDir()
		binary := filepath.Join(directory, "devtools.exe")
		arch, err := NativeArchitecture()
		if err != nil {
			t.Fatal(err)
		}
		commit := os.Getenv("GITHUB_SHA")
		if commit == "" {
			commit = "fixture"
		}
		flags := "-s -w -X main.packageIdentity=devtools-build-v1:0.0.0-native-package:" + commit + ":windows:" + arch + ":end"
		command := exec.Command("go", "build", "-trimpath", "-ldflags", flags, "-o", binary, "../../cmd/devtools")
		command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=windows", "GOARCH="+arch)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("real candidate build: %s %v", output, err)
		}
		body, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		checkRealPackage(t, body, commit)
		return
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 {
		t.Fatal("fixture ZIP shape")
	}
	member, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	body, err := readBounded(member, maxImage)
	member.Close()
	if err != nil {
		t.Fatal(err)
	}
	checkRealPackage(t, body, os.Getenv("GITHUB_SHA"))
}

func checkRealPackage(t *testing.T, body []byte, commit string) {
	t.Helper()
	arch, err := NativeArchitecture()
	if err != nil {
		t.Fatal(err)
	}
	id, err := inspectImage(body, "0.0.0-native-package", arch)
	if err != nil || id.Commit != commit {
		t.Fatalf("real release identity: %+v %v", id, err)
	}
	if _, err := inspectImage(body, "0.0.0-test.9", arch); err == nil {
		t.Fatal("wrong version accepted")
	}
	other := append([]byte{}, body...)
	offset := binary.LittleEndian.Uint32(other[60:64])
	machine := uint16(0xaa64)
	if arch == "arm64" {
		machine = 0x8664
	}
	binary.LittleEndian.PutUint16(other[offset+4:offset+6], machine)
	if _, err := inspectImage(other, "0.0.0-native-package", arch); err == nil {
		t.Fatal("wrong CPU accepted")
	}
}
func TestWindowsInstallBootstrapInputIsBoundedAndStrict(t *testing.T) {
	valid, err := DecodeBootstrap(strings.NewReader(`{"action":"install","version":"0.1.0","bin_dir":"chosen","external_alias":true}`))
	if err != nil || valid.Version != "0.1.0" || valid.BinDir != "chosen" || !valid.ExternalAlias {
		t.Fatalf("bootstrap decode: %+v %v", valid, err)
	}
	for _, body := range []string{`{"action":"install","command":"arbitrary"}`, `{} {}`, strings.Repeat("x", 4097)} {
		if _, err := DecodeBootstrap(strings.NewReader(body)); err == nil {
			t.Fatal("invalid bootstrap input accepted")
		}
	}
}
