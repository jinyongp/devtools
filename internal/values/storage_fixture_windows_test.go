package values

import (
	"context"
	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
	"os"
	"testing"
)

func assertPrivateStoredPath(t *testing.T, path string) {
	t.Helper()
	info, e := os.Lstat(path)
	if e != nil {
		t.Fatal(e)
	}
	if info.IsDir() {
		e = platformfs.CheckDirectory(path)
	} else {
		_, e = platformfs.StatPrivate(path)
	}
	if e != nil {
		t.Fatal("private ACL fixture failed", e)
	}
}
func assertUnsafeStoreRejected(t *testing.T, store Store) {
	t.Helper()
	sd, e := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if e != nil {
		t.Fatal(e)
	}
	acl, _, e := sd.DACL()
	if e != nil {
		t.Fatal(e)
	}
	if e = windows.SetNamedSecurityInfo(store.file(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := store.Read(context.Background()); e == nil || e.Code != "storage_error" {
		t.Fatal("accepted public file")
	}
}
func holdProfileLock(t *testing.T, path string) func() {
	t.Helper()
	f, e := platformfs.OpenPrivate(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if ok, e := f.TryLock(false); e != nil || !ok {
		t.Fatal("fixture lock failed", e)
	}
	return func() { _ = f.Unlock(); _ = f.Close() }
}
