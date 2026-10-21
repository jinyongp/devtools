package platformfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/jinyongp/devtools/internal/storageerr"
	"golang.org/x/sys/windows"
)

func TestWindowsContainmentUsesEntryIdentityNotFileLabel(t *testing.T) {
	root := testRoot(t)
	if err := root.MkdirAll("nested"); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join("nested", "state")
	if err := root.WriteFile(context.Background(), name, []byte("original"), false); err != nil {
		t.Fatal(err)
	}
	file, err := root.OpenFile(name, os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// Inherited handles have arbitrary labels. Their identity must be checked
	// against the requested entry, not os.File.Name or a normalized path string.
	var duplicate windows.Handle
	process := windows.CurrentProcess()
	if err = windows.DuplicateHandle(process, windows.Handle(file.Fd()), process, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		t.Fatal(err)
	}
	label := os.NewFile(uintptr(duplicate), "inherited-handle")
	defer label.Close()
	if err = containedFile(root, label, filepath.Join(root.path, name)); err != nil {
		t.Fatal(err)
	}
	if err = root.ValidateFileHandle(name, label); err != nil {
		t.Fatal(err)
	}
	if err = containedFile(root, label, filepath.Join(filepath.Dir(root.path), "outside")); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("outside entry accepted: %v", err)
	}
}

func TestWindowsContainmentRejectsDifferentEntryInSameRoot(t *testing.T) {
	root := testRoot(t)
	for _, name := range []string{"expected", "other"} {
		if err := root.WriteFile(context.Background(), name, []byte("same data"), false); err != nil {
			t.Fatal(err)
		}
	}
	file, err := root.OpenFile("other", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = containedFile(root, file.File, filepath.Join(root.path, "expected")); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("different file accepted: %v", err)
	}
	details := storageerr.Details(err, "tasks", "read_file", "task_store")
	if details["operation"] != "verify_file_identity" || details["reason"] != "file_identity_mismatch" {
		t.Fatal("identity failure misclassified", details)
	}
}

func TestWindowsContainmentRejectsIntermediateReparse(t *testing.T) {
	root, outside := testRoot(t), testRoot(t)
	file, err := outside.OpenFile("state", os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	alias := filepath.Join(root.path, "alias")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, outside.path).CombinedOutput(); err != nil {
		t.Fatalf("junction fixture: %v %s", err, output)
	}
	defer os.Remove(alias)
	err = containedFile(root, file.File, filepath.Join(alias, "state"))
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("followed intermediate junction: %v", err)
	}
}

func TestWindowsContainmentReopensLongNestedEntry(t *testing.T) {
	root := testRoot(t)
	nested := strings.Repeat(strings.Repeat("a", 80)+`\`, 7)
	if err := root.MkdirAll(nested); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(nested, "state")
	if err := root.WriteFile(context.Background(), name, []byte("long path"), false); err != nil {
		t.Fatal(err)
	}
	body, err := root.ReadFile(name, 100)
	if err != nil || string(body) != "long path" {
		t.Fatalf("long entry: %q %v", body, err)
	}
}

func TestWindowsExistingStorageReopensThroughCaseAlias(t *testing.T) {
	root := testRoot(t)
	if err := root.WriteFile(context.Background(), "state", []byte("existing"), false); err != nil {
		t.Fatal(err)
	}
	path := strings.ToUpper(root.path)
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRoot(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	data, err := reopened.ReadFile("STATE", 100)
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing case alias: %q %v", data, err)
	}
}

func TestWindowsContainmentPreservesCaseSensitiveEntries(t *testing.T) {
	root := testRoot(t)
	ptr, err := pathPtr(root.path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(ptr, windows.FILE_WRITE_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	flag := uint32(1) // FILE_CS_FLAG_CASE_SENSITIVE_DIR; FileCaseSensitiveInfo = 23.
	err = windows.SetFileInformationByHandle(h, 23, (*byte)(unsafe.Pointer(&flag)), uint32(unsafe.Sizeof(flag)))
	_ = windows.CloseHandle(h)
	if errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		t.Skipf("per-directory case sensitivity unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"State", "state"} {
		if err = root.WriteFile(context.Background(), name, []byte(name), false); err != nil {
			t.Fatal(err)
		}
	}
	file, err := root.OpenFile("State", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = containedFile(root, file.File, filepath.Join(root.path, "State")); err != nil {
		t.Fatal(err)
	}
	if err = containedFile(root, file.File, filepath.Join(root.path, "state")); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("different case-sensitive entry accepted: %v", err)
	}
}
