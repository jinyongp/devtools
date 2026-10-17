package platformfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsOpenRootPinsItsOwnDirectory(t *testing.T) {
	root := testRoot(t)
	if err := os.Rename(root.path, root.path+"-moved"); err == nil {
		t.Fatal("open root's own directory could be renamed")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root.path, root.path+"-moved"); err != nil {
		t.Fatal(err)
	}
}
func TestWindowsExternalPrivateFileKeepsPublicParentACL(t *testing.T) {
	parent := t.TempDir()
	before, err := windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "archive.age")
	if err = WriteExternalPrivate(context.Background(), path, []byte("encrypted"), false); err != nil {
		t.Fatal(err)
	}
	body, err := ReadExternalPrivate(path, 20)
	if err != nil || string(body) != "encrypted" {
		t.Fatalf("read: %q %v", body, err)
	}
	if err = WriteExternalPrivate(context.Background(), path, []byte("replacement"), false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite allowed: %v", err)
	}
	if err = EnsureExternalDirectory(parent); err != nil {
		t.Fatal(err)
	}
	after, err := windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("external parent ACL changed")
	}
}
func exclusiveWindowsReader(t *testing.T, path string, directory bool) windows.Handle {
	t.Helper()
	ptr, err := pathPtr(path)
	if err != nil {
		t.Fatal(err)
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	h, err := windows.CreateFile(ptr, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestWindowsPublicationRetriesAnExclusiveTargetReader(t *testing.T) {
	root := testRoot(t)
	ctx := context.Background()
	if err := root.WriteFile(ctx, "state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	h := exclusiveWindowsReader(t, filepath.Join(root.path, "state"), false)
	canceled, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	err := root.WriteFile(canceled, "state", []byte("new"), true)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		windows.CloseHandle(h)
		t.Fatalf("reader retry cancellation: %v", err)
	}
	closed := make(chan error, 1)
	go func() { time.Sleep(40 * time.Millisecond); closed <- windows.CloseHandle(h) }()
	err = root.WriteFile(ctx, "state", []byte("new"), true)
	if closeErr := <-closed; closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	body, err := root.ReadFile("state", 10)
	if err != nil || string(body) != "new" {
		t.Fatalf("publication: %q %v", body, err)
	}
}
func TestWindowsPrivateRemovalBoundsSharingAndCancellation(t *testing.T) {
	root := testRoot(t)
	ctx := context.Background()
	if err := root.WriteFile(ctx, "state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	reader, err := root.OpenFile("state", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	canceled, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	err = root.RemoveFile(canceled, "state")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("removal under reader: %v", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err = root.RemoveFile(ctx, "state"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root.path, "state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestWindowsDirectoryPublicationRetriesExclusiveReader(t *testing.T) {
	root := testRoot(t)
	if err := root.MkdirAll("staging"); err != nil {
		t.Fatal(err)
	}
	h := exclusiveWindowsReader(t, filepath.Join(root.path, "staging"), true)
	closed := make(chan error, 1)
	go func() { time.Sleep(40 * time.Millisecond); closed <- windows.CloseHandle(h) }()
	err := root.PublishDirectory(context.Background(), "staging", "published")
	if closeErr := <-closed; closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
}
