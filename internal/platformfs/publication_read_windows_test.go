package platformfs

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsReadWaitsForPublicationDeleteHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	root, err := OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.WriteFile(context.Background(), "record.json", []byte(`{"snapshot":"complete"}`), false); err != nil {
		t.Fatal(err)
	}
	pointer, err := windows.UTF16PtrFromString(filepath.Join(path, "record.json"))
	if err != nil {
		t.Fatal(err)
	}
	// This is the access/share conflict produced by MoveFileEx publication.
	handle, err := windows.CreateFile(pointer, windows.DELETE|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
	}()
	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)
	go func() { body, err := root.ReadFile("record.json", 1024); done <- result{body, err} }()
	select {
	case got := <-done:
		t.Fatal("read did not wait for publisher", got.err)
	case <-time.After(80 * time.Millisecond):
	}
	if err = windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	handle = 0
	select {
	case got := <-done:
		if got.err != nil || string(got.body) != `{"snapshot":"complete"}` {
			t.Fatal(string(got.body), got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not resume after publisher closed")
	}
	// Security errors still fail closed through the ordinary checker.
	public := filepath.Join(t.TempDir(), "public.json")
	if err = os.WriteFile(public, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if file, err := OpenExternalPrivate(public, os.O_RDONLY, 0); err == nil {
		file.Close()
		t.Fatal("private leaf ACL requirement weakened")
	}
}
