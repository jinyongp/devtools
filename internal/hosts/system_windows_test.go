package hosts

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func hostsSecurity(t *testing.T, path string) string {
	t.Helper()
	release, err := enableHostsMetadataPrivileges()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(ptr, windows.READ_CONTROL|windows.ACCESS_SYSTEM_SECURITY, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	security, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.SACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	value := security.String()
	if value == "" {
		t.Fatal("cannot serialize hosts security descriptor")
	}
	return value
}
func TestWindowsHostsExpectedDigestACLAndCRLFPreservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	before := []byte("# external comment\r\n127.0.0.1 existing.localhost\r\n")
	if err := os.WriteFile(path, before, 0644); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	security := hostsSecurity(t, path)
	system := System{Path: path}
	request := Request{Expected: digest(before), Mappings: []Mapping{{Host: "native.localhost", Address: "127.0.0.1"}}}
	if failure := system.Apply(context.Background(), request); failure != nil {
		t.Fatalf("%s: %v", failure.Code, failure.Details)
	}
	actual, failure := system.Read()
	if failure != nil || !bytes.HasPrefix(actual, before) || !bytes.Contains(actual, []byte("127.0.0.1\tnative.localhost\r\n")) {
		t.Fatalf("hosts external bytes/newline: %q %v", actual, failure)
	}
	afterSecurity := hostsSecurity(t, path)
	if afterSecurity != security {
		t.Fatal("hosts owner/group/DACL/SACL changed")
	}
	if failure = system.Apply(context.Background(), request); failure == nil || failure.Code != "stale_preview" {
		t.Fatalf("stale preview accepted: %v", failure)
	}
	if failure = system.Apply(context.Background(), Request{Expected: digest(actual)}); failure != nil {
		t.Fatalf("%s: %v", failure.Code, failure.Details)
	}
	restored, failure := system.Read()
	if failure != nil || !bytes.Equal(restored, before) {
		t.Fatalf("last mapping cleanup lost external bytes: %q %v", restored, failure)
	}
}
func TestWindowsHostsCanceledAndReparseTargetsStayUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	body := []byte("# original\n")
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failure := (System{Path: path}).Apply(ctx, Request{Expected: digest(body)}); failure == nil || failure.Code != "canceled" {
		t.Fatalf("%s: %v", failure.Code, failure.Details)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, failure := (System{Path: alias}).Read(); failure == nil {
		t.Fatal("reparse hosts target read")
	}
	if failure := (System{Path: alias}).Apply(context.Background(), Request{Expected: digest(body), Mappings: []Mapping{{Host: "native.localhost", Address: "127.0.0.1"}}}); failure == nil {
		t.Fatal("reparse hosts target changed")
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, body) {
		t.Fatal("refused target changed")
	}
}
func TestWindowsHostsTargetIsFixedByWindowsDirectory(t *testing.T) {
	directory, err := windows.GetWindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if got := (System{}).path(); got != filepath.Join(directory, "System32", "drivers", "etc", "hosts") {
		t.Fatal(got)
	}
	t.Setenv("SystemRoot", t.TempDir())
	if got := (System{}).path(); got != filepath.Join(directory, "System32", "drivers", "etc", "hosts") {
		t.Fatal("target followed environment override")
	}
}

func TestWindowsSystemHostsRestoresDisposableRunner(t *testing.T) {
	if os.Getenv("DEVTOOLS_TEST_SYSTEM_NETWORK") != "1" {
		t.Skip("requires disposable native OS hosts fixture")
	}
	system := System{}
	before, failure := system.Read()
	if failure != nil {
		t.Fatalf("%s: %v", failure.Code, failure.Details)
	}
	if bytes.Contains(before, []byte(begin)) || bytes.Contains(before, []byte(end)) {
		t.Fatal("disposable runner already has an owned hosts block")
	}
	security := hostsSecurity(t, system.path())
	request := Request{Expected: digest(before), Mappings: []Mapping{{Host: "devtools-ci-hosts.localhost", Address: "127.0.0.1"}}}
	t.Cleanup(func() {
		current, failure := system.Read()
		if failure != nil {
			t.Error(failure)
			return
		}
		if failure = system.Apply(context.Background(), Request{Expected: digest(current)}); failure != nil {
			t.Error(failure)
			return
		}
		restored, failure := system.Read()
		if failure != nil || !bytes.Equal(restored, before) {
			t.Error("disposable system hosts not restored", failure)
		}
		if hostsSecurity(t, system.path()) != security {
			t.Error("system hosts ACL/owner not restored")
		}
	})
	if failure = system.Apply(context.Background(), request); failure != nil {
		t.Fatalf("%s: %v", failure.Code, failure.Details)
	}

	current, failure := system.Read()
	if failure != nil || !bytes.HasPrefix(current, before) || !bytes.Contains(current, []byte("devtools-ci-hosts.localhost")) {
		t.Fatalf("system hosts mutation: %q %v", current, failure)
	}
	if hostsSecurity(t, system.path()) != security {
		t.Fatal("system hosts owner/group/DACL/SACL changed")
	}
}

func TestWindowsHostsRefusesOutputThatWouldBecomeUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	before := append(bytes.Repeat([]byte("#"), (1<<20)-2), '\n')
	if err := os.WriteFile(path, before, 0644); err != nil {
		t.Fatal(err)
	}
	failure := (System{Path: path}).Apply(context.Background(), Request{Expected: digest(before), Mappings: []Mapping{{Host: "native.localhost", Address: "127.0.0.1"}}})
	if failure == nil || failure.Code != "invalid_argument" {
		t.Fatalf("oversized output accepted: %v", failure)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, before) {
		t.Fatal("oversized hosts request changed original", err)
	}
}
