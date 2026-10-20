package platformfs_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

// The parent launches this same test under a real, disposable AppContainer.
// The profile and every fixture belong to this test, never to a user's store.
func TestWindowsAppContainerPrivateStorage(t *testing.T) {
	if args := flag.Args(); len(args) == 1 && args[0] == "devtools-appcontainer-child" {
		testAppContainerStorage(t)
		return
	}
	userenv := windows.NewLazySystemDLL("userenv.dll")
	create := userenv.NewProc("CreateAppContainerProfile")
	remove := userenv.NewProc("DeleteAppContainerProfile")
	folder := userenv.NewProc("GetAppContainerFolderPath")
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	name := "devtools-test-" + hex.EncodeToString(nonce)
	namePtr, _ := windows.UTF16PtrFromString(name)
	var sid *windows.SID
	hr, _, _ := create.Call(uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(namePtr)), 0, 0, uintptr(unsafe.Pointer(&sid)))
	if uint32(hr) != 0 {
		t.Fatalf("CreateAppContainerProfile HRESULT 0x%08x", uint32(hr))
	}
	defer windows.FreeSid(sid)
	var profile string
	defer func() {
		hr, _, _ := remove.Call(uintptr(unsafe.Pointer(namePtr)))
		if uint32(hr) != 0 {
			t.Errorf("DeleteAppContainerProfile %s HRESULT 0x%08x", name, uint32(hr))
		}
		if profile != "" {
			if _, err := os.Lstat(profile); !os.IsNotExist(err) {
				t.Errorf("temporary AppContainer remains at %s: %v", profile, err)
			}
		}
	}()
	sidPtr, _ := windows.UTF16PtrFromString(sid.String())
	var folderPtr *uint16
	hr, _, _ = folder.Call(uintptr(unsafe.Pointer(sidPtr)), uintptr(unsafe.Pointer(&folderPtr)))
	if uint32(hr) != 0 {
		t.Fatalf("GetAppContainerFolderPath HRESULT 0x%08x", uint32(hr))
	}
	profile = windows.UTF16PtrToString(folderPtr)
	windows.CoTaskMemFree(unsafe.Pointer(folderPtr))
	outside := filepath.Join(profile, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", filepath.Join(profile, "alias"), outside).CombinedOutput(); err != nil {
		t.Fatalf("AppContainer junction fixture: %v %s", err, output)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(profile, "platformfs.test.exe")
	if err := os.WriteFile(exe, body, 0700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(profile, "test.log")
	logPtr, _ := windows.UTF16PtrFromString(logPath)
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	log, err := windows.CreateFile(logPtr, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(log)
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		t.Fatal(err)
	}
	defer attributes.Delete()
	capabilities := struct {
		SID             *windows.SID
		Capabilities    *windows.SIDAndAttributes
		Count, Reserved uint32
	}{SID: sid}
	if err := attributes.Update(0x20009, unsafe.Pointer(&capabilities), unsafe.Sizeof(capabilities)); err != nil {
		t.Fatal(err)
	}
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&log), unsafe.Sizeof(log)); err != nil {
		t.Fatal(err)
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.StdInput, startup.StdOutput, startup.StdErr = log, log, log
	exePtr, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{exe, "-test.run=^TestWindowsAppContainerPrivateStorage$", "-test.v", "--", "devtools-appcontainer-child"}))
	cwd, _ := windows.UTF16PtrFromString(profile)
	var process windows.ProcessInformation
	if err := windows.CreateProcess(exePtr, args, nil, nil, true, windows.EXTENDED_STARTUPINFO_PRESENT, nil, cwd, &startup.StartupInfo, &process); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process.Process)
	windows.CloseHandle(process.Thread)
	wait, err := windows.WaitForSingleObject(process.Process, 30000)
	if err != nil || wait != windows.WAIT_OBJECT_0 {
		windows.TerminateProcess(process.Process, 2)
		windows.WaitForSingleObject(process.Process, 5000)
		t.Fatalf("AppContainer child did not finish: wait=%d error=%v", wait, err)
	}
	var exit uint32
	if err := windows.GetExitCodeProcess(process.Process, &exit); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if exit != 0 {
		t.Fatalf("AppContainer child exit=%d\n%s", exit, body)
	}
	t.Logf("AppContainer child:\n%s", body)
}

func testAppContainerStorage(t *testing.T) {
	var app, size uint32
	err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), 29, (*byte)(unsafe.Pointer(&app)), uint32(unsafe.Sizeof(app)), &size)
	if err != nil || app == 0 {
		t.Fatalf("not running in AppContainer: %v", err)
	}
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "devtools", "data")
	start, err := platformfs.WindowsPrivatePathBoundary(path)
	if err != nil || !strings.EqualFold(start, base) {
		t.Fatalf("container boundary not selected: %q %v", start, err)
	}
	start, err = platformfs.WindowsPrivatePathBoundary(filepath.Join(base+"-other", "data"))
	if err != nil || start != filepath.VolumeName(base)+`\` {
		t.Fatalf("container boundary escaped into sibling: %q %v", start, err)
	}
	if unsafe, err := platformfs.OpenRoot(filepath.Join(base, "alias", "private"), true); !errors.Is(err, platformfs.ErrUnsafe) {
		if unsafe != nil {
			unsafe.Close()
		}
		t.Fatalf("accepted AppContainer reparse ancestor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "outside", "private")); !os.IsNotExist(err) {
		t.Fatalf("created storage through rejected reparse point: %v", err)
	}
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.MkdirAll("nested"); err != nil {
		t.Fatal(err)
	}
	if err := platformfs.MkdirPrivate(filepath.Join(path, "private-child")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := root.WriteFile(ctx, "state", []byte("first"), false); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(ctx, "state", []byte("second"), true); err != nil {
		t.Fatal(err)
	}
	body, err := root.ReadFile("state", 32)
	if err != nil || string(body) != "second" {
		t.Fatalf("read after publication: %q %v", body, err)
	}
	lease, err := root.OpenExclusive("lease", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.ValidateFileHandle("lease", lease.File); err != nil {
		t.Fatal(err)
	}
	if err := platformfs.CheckFile(lease.File); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := root.OpenFile("lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := lock.TryLock(false)
	if !ok || err != nil {
		t.Fatalf("lock: acquired=%t %v", ok, err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	private, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	public, err := windows.SecurityDescriptorFromString(private.String() + "(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	set := func(sd *windows.SECURITY_DESCRIPTOR) {
		t.Helper()
		acl, _, err := sd.DACL()
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
			t.Fatal(err)
		}
	}
	set(public)
	before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	// Case variation must not bypass ACL checks when the Known Folder supplies
	// a differently cased path prefix. Existing unsafe objects are never repaired.
	if unsafe, err := platformfs.OpenRoot(strings.ToLower(path), true); !errors.Is(err, platformfs.ErrUnsafe) {
		if unsafe != nil {
			unsafe.Close()
		}
		t.Fatalf("accepted insecure existing storage: %v", err)
	}
	after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("existing ACL repaired implicitly")
	}
	set(private)
	reopened, err := platformfs.OpenRoot(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(base, "isolated")
	dirs, err := paths.Isolated(isolated)
	if err != nil {
		t.Fatalf("isolated path resolution: %v", err)
	}
	if _, err := os.Stat(isolated); !os.IsNotExist(err) {
		t.Fatalf("path resolution created isolated root: %v", err)
	}
	if dirs.IsolatedRoot != isolated || dirs.Data != filepath.Join(isolated, "data") {
		t.Fatalf("unexpected isolated directories: %+v", dirs)
	}
	isolatedRoot, err := platformfs.OpenRoot(dirs.Data, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := isolatedRoot.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := paths.Isolated(filepath.Join(base, "alias", "isolated")); !errors.Is(err, platformfs.ErrUnsafe) {
		t.Fatalf("isolated reparse ancestor accepted: %v", err)
	}
	fmt.Fprintln(os.Stdout, "private directories, publication, reopening, locks, leases, reparse rejection and unsafe ACL preservation passed")
}
