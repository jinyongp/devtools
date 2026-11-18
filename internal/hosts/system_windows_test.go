package hosts

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
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
	// Independently remove only the documented child-propagation header bits.
	// Rights, ACE flags (including ID), owners and P/AR flags remain exact.
	for _, pair := range [][2]string{{"D:PAI", "D:P"}, {"D:AI", "D:"}, {"S:PAI", "S:P"}, {"S:AI", "S:"}} {
		value = strings.ReplaceAll(value, pair[0], pair[1])
	}
	return value
}
func TestWindowsHostsExpectedDigestACLAndCRLFPreservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	before := []byte("# external comment\r\n127.0.0.1 existing.localhost\r\n")
	if err := os.WriteFile(path, before, 0644); err != nil {
		t.Fatal(err)
	}
	prepareHostsTestFile(t, path)
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
	if expected := os.Getenv("DEVTOOLS_TEST_HOSTS_EXPECTED"); expected != "" {
		gate := os.Getenv("DEVTOOLS_TEST_HOSTS_GATE")
		wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			select {
			case <-wait.Done():
				t.Fatal("writer gate did not open")
			case <-time.After(10 * time.Millisecond):
			}
		}
		request := Request{Expected: expected, Mappings: []Mapping{{Host: os.Getenv("DEVTOOLS_TEST_HOSTS_MAPPING"), Address: "127.0.0.1"}}}
		ctx, cancelWrite := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelWrite()
		e := system.Apply(ctx, request)
		switch {
		case e == nil:
			t.Log("HOSTS_RESULT=applied")
		case e.Code == "stale_preview":
			t.Log("HOSTS_RESULT=stale")
		case e.Code == "canceled":
			t.Log("HOSTS_RESULT=canceled")
		default:
			t.Fatalf("%s: %v", e.Code, e.Details)
		}
		return
	}
	before, failure := system.Read()
	if failure != nil {
		t.Fatalf("%s: %v", failure.Code, failure.Details)
	}
	if bytes.Contains(before, []byte(begin)) || bytes.Contains(before, []byte(end)) {
		t.Fatal("disposable runner already has an owned hosts block")
	}
	security := hostsSecurity(t, system.path())
	t.Cleanup(func() {
		current, failure := system.Read()
		if failure != nil {
			t.Errorf("%s: %v", failure.Code, failure.Details)
			return
		}
		if failure = system.Apply(context.Background(), Request{Expected: digest(current)}); failure != nil {
			t.Errorf("%s: %v", failure.Code, failure.Details)
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
	gate := filepath.Join(t.TempDir(), "start")
	worker := func(host string) *exec.Cmd {
		command := exec.Command(os.Args[0], "-test.v", "-test.run", "^TestWindowsSystemHostsRestoresDisposableRunner$")
		command.Env = append(os.Environ(), "DEVTOOLS_TEST_HOSTS_EXPECTED="+digest(before), "DEVTOOLS_TEST_HOSTS_GATE="+gate, "DEVTOOLS_TEST_HOSTS_MAPPING="+host)
		return command
	}
	checkLock := func() {
		release, err := enableHostsMetadataPrivileges()
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		file, err := platformfs.OpenPublicNoFollow(system.path(), true)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		handle := windows.Handle(file.Fd())
		lock := windows.Overlapped{Offset: 2 * maxHostsSize}
		if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &lock); err != nil {
			t.Fatal(err)
		}
		defer windows.UnlockFileEx(handle, 0, 1, 0, &lock)
		if err := os.WriteFile(gate, nil, 0600); err != nil {
			t.Fatal(err)
		}
		result := make(chan []byte, 1)
		go func() {
			body, _ := system.Read()
			result <- body
		}()
		select {
		case body := <-result:
			if !bytes.Equal(body, before) {
				t.Fatal("hosts read changed under helper lock")
			}
		case <-time.After(time.Second):
			t.Fatal("hosts read blocked behind the helper lock")
		}
		output, err := worker("devtools-ci-hosts-canceled.test").CombinedOutput()
		if err != nil || !bytes.Contains(output, []byte("HOSTS_RESULT=canceled")) {
			t.Fatalf("waiting helper did not cancel: %v %s", err, output)
		}
		t.Log("Native hosts readers and lock cancellation verified")
	}
	checkLock()
	if err := os.Remove(gate); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		output []byte
		err    error
	}
	results := make(chan outcome, 2)
	for _, host := range []string{"devtools-ci-hosts-one.test", "devtools-ci-hosts-two.test"} {
		go func() {
			output, err := worker(host).CombinedOutput()
			results <- outcome{output, err}
		}()
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	applied, stale := 0, 0
	for range 2 {
		result := <-results
		switch {
		case result.err != nil:
			t.Fatal(result.err, string(result.output))
		case bytes.Contains(result.output, []byte("HOSTS_RESULT=applied")):
			applied++
		case bytes.Contains(result.output, []byte("HOSTS_RESULT=stale")):
			stale++
		default:
			t.Fatal(string(result.output))
		}
	}
	if applied != 1 || stale != 1 {
		t.Fatal("expected one publication and one stale concurrent writer", applied, stale)
	}
	t.Log("Two concurrent native hosts writers produced only one publication")
	current, failure := system.Read()
	if failure != nil || !bytes.HasPrefix(current, before) || bytes.Count(current, []byte("devtools-ci-hosts-")) != 1 {
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

func TestWindowsHostsSecurityComparisonKeepsEveryPermissionBoundary(t *testing.T) {
	baseline, err := windows.SecurityDescriptorFromString("O:SYG:SYD:PAI(A;;FR;;;BU)S:AINO_ACCESS_CONTROL")
	if err != nil {
		t.Fatal(err)
	}
	want, err := comparableHostsSecurity(baseline)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		descriptor string
		equal      bool
	}{
		{"O:SYG:SYD:P(A;;FR;;;BU)S:NO_ACCESS_CONTROL", true},
		{"O:SYG:SYD:P(A;;FW;;;BU)S:NO_ACCESS_CONTROL", false},
		{"O:SYG:SYD:(A;;FR;;;BU)S:NO_ACCESS_CONTROL", false},
		{"O:SYG:SYD:P(A;ID;FR;;;BU)S:NO_ACCESS_CONTROL", false},
		{"O:BAG:SYD:P(A;;FR;;;BU)S:NO_ACCESS_CONTROL", false},
		{"O:SYG:BAD:P(A;;FR;;;BU)S:NO_ACCESS_CONTROL", false},
		{"O:SYG:SYD:P(A;;FR;;;BU)S:P(AU;SA;FR;;;WD)", false},
	} {
		sd, err := windows.SecurityDescriptorFromString(test.descriptor)
		if err != nil {
			t.Fatal(err)
		}
		got, err := comparableHostsSecurity(sd)
		if err != nil || (got == want) != test.equal {
			t.Fatal("permission equivalence mismatch", test.descriptor, err)
		}
	}
}

func prepareHostsTestFile(t *testing.T, path string) {
	t.Helper()
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
}
func assertHostsTestPermissions(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	actual := strings.ReplaceAll(sd.String(), "D:PAI", "D:P")
	if actual != expected.String() {
		t.Fatal("hosts fixture DACL changed", actual)
	}
}
