package platformfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsRejectsAmbiguousPaths(t *testing.T) {
	for _, path := range []string{`C:private`, `\\server\share\private`, `\\?\C:\private`, `C:\private:stream`, `C:\private.`, `C:\private `, `C:\NUL`, `C:\CON.txt`} {
		if _, err := privatePath(path); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("unsafe path accepted: %q %v", path, err)
		}
	}
}
func TestWindowsRejectsPublicRootWithoutRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inherited")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if root, err := OpenRoot(path, true); err == nil {
		root.Close()
		t.Fatal("accepted inherited/public root")
	}
	after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("existing root permissions were repaired implicitly")
	}
}
func TestWindowsParentRenameBlocked(t *testing.T) {
	base := t.TempDir()
	root, err := OpenRoot(filepath.Join(base, "private"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(base, base+"-moved"); err == nil {
		t.Fatal("pinned ancestor renamed")
	}
}
func TestWindowsReparseParentRejected(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	alias := filepath.Join(base, "alias")
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, outside)
	if err := cmd.Run(); err != nil {
		t.Fatalf("junction fixture: %v", err)
	}
	defer os.Remove(alias)
	if root, err := OpenRoot(filepath.Join(alias, "private"), true); err == nil {
		root.Close()
		t.Fatal("junction ancestor accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "private")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created a directory beyond rejected junction")
	}
}
func TestWindowsReaderRetryAndCancel(t *testing.T) {
	root := testRoot(t)
	if err := root.WriteFile(context.Background(), "state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	reader, err := root.OpenFile("state", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err = root.WriteFile(ctx, "state", []byte("new"), true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("live reader replacement: %v", err)
	}
	data, err := root.ReadFile("state", 10)
	if err != nil || string(data) != "old" {
		t.Fatalf("failed replacement: %q %v", data, err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile(context.Background(), "state", []byte("new"), true); err != nil {
		t.Fatal(err)
	}
}
func TestWindowsOpenFilePinsParentAfterRootClose(t *testing.T) {
	root := testRoot(t)
	f, err := root.OpenFile("state", os.O_CREATE|os.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(root.path, root.path+"-moved"); err == nil {
		t.Fatal("file lost its checked parent guard")
	}
}

func TestWindowsUnsafeTargetIsNotTruncatedOrReplaced(t *testing.T) {
	root := testRoot(t)
	if err := root.WriteFile(context.Background(), "state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.path, "state")
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if f, err := root.OpenFile("state", os.O_WRONLY|os.O_TRUNC); err == nil {
		f.Close()
		t.Fatal("insecure target truncated")
	}
	if err = root.WriteFile(context.Background(), "state", []byte("new"), true); err == nil {
		t.Fatal("insecure target replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("insecure original was changed: %q %v", data, err)
	}
}
func TestWindowsHardLinkCannotBypassPrivateFileCheck(t *testing.T) {
	root := testRoot(t)
	if err := root.WriteFile(context.Background(), "state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "outside")
	if err := os.Link(filepath.Join(root.path, "state"), alias); err != nil {
		t.Fatal(err)
	}
	if f, err := root.OpenFile("state", os.O_WRONLY|os.O_TRUNC); err == nil {
		f.Close()
		t.Fatal("multi-link private file accepted")
	}
	data, err := os.ReadFile(alias)
	if err != nil || string(data) != "old" {
		t.Fatalf("outside link changed: %q %v", data, err)
	}
}

// The hosted disposable runner supplies a temporary standard account. Local
// non-admin runs can omit it; the Windows boundary CI always requires this check.
func TestWindowsOtherIdentityDenied(t *testing.T) {
	user, password := os.Getenv("DEVTOOLS_TEST_USER"), os.Getenv("DEVTOOLS_TEST_PASSWORD")
	if user == "" || password == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("separate-identity fixture is required in CI")
		}
		t.Skip("separate standard account not supplied on local host")
	}
	base := filepath.Join(os.Getenv("SystemRoot"), "Temp", "devtools-acl-"+filepath.Base(t.TempDir()))
	root, err := OpenRoot(base, true)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	defer root.Close()
	if err = root.WriteFile(context.Background(), "private", []byte("public-test-canary"), false); err != nil {
		t.Fatal(err)
	}
	userPtr, _ := windows.UTF16PtrFromString(user)
	domainPtr, _ := windows.UTF16PtrFromString(".")
	passwordPtr, _ := windows.UTF16PtrFromString(password)
	logon := windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW")
	var token windows.Token
	ok, _, e := logon.Call(uintptr(unsafe.Pointer(userPtr)), uintptr(unsafe.Pointer(domainPtr)), uintptr(unsafe.Pointer(passwordPtr)), 2, 0, uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		t.Fatalf("standard-account token: %v", e)
	}
	defer token.Close()
	var impersonation windows.Token
	if err = windows.DuplicateTokenEx(token, windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE, nil, windows.SecurityImpersonation, windows.TokenImpersonation, &impersonation); err != nil {
		t.Fatal(err)
	}
	defer impersonation.Close()
	path, _ := pathPtr(filepath.Join(base, "private"))
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err = windows.SetThreadToken(nil, impersonation); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.RevertToSelf(); err != nil {
			t.Fatal(err)
		}
	}()
	h, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		windows.CloseHandle(h)
		t.Fatal("different identity read private file")
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("expected access denied: %v", err)
	}
}
