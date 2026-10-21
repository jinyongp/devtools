package vault

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() { nativePeerFixture = runWindowsPeerFixture }

// A deliberately public fixture pipe lets the peer check run independently of
// the production pipe ACL. No key, session endpoint file or request uses it.
func nativePeerTestPipe(name string) (windows.Handle, error) {
	return nativePeerPipe(name, "D:P(A;;GA;;;WD)", true)
}

func nativePeerPipe(name, descriptor string, rejectRemote bool) (windows.Handle, error) {
	sd, err := windows.SecurityDescriptorFromString(descriptor)
	if err != nil {
		return 0, err
	}
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	mode := uint32(windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT)
	if rejectRemote {
		mode |= windows.PIPE_REJECT_REMOTE_CLIENTS
	}
	return windows.CreateNamedPipe(ptr, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		mode,
		1, 4096, 4096, 0, &attrs)
}

func runWindowsPeerFixture(mode, endpoint string) int {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var handle windows.Handle
	switch mode {
	case "server":
		handle, err = nativePeerTestPipe(endpoint)
		if err != nil {
			return 3
		}
		if reportWindowsPeerSID(endpoint, user.User.Sid.String()) != 0 {
			windows.CloseHandle(handle)
			return 11
		}
		if err := connectSessionPipe(ctx, handle); err != nil {
			windows.CloseHandle(handle)
			return 4
		}
	case "client", "denied-client":
		ptr, err := windows.UTF16PtrFromString(endpoint)
		if err != nil {
			return 5
		}
		handle, err = windows.CreateFile(ptr, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
			0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if mode == "denied-client" {
			if err == nil {
				windows.CloseHandle(handle)
				return 9
			}
			if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				return 10
			}
			return reportWindowsPeerSID(endpoint, user.User.Sid.String())
		}
		if err != nil {
			return 6
		}
	default:
		return 7
	}
	if mode == "client" && reportWindowsPeerSID(endpoint, user.User.Sid.String()) != 0 {
		windows.CloseHandle(handle)
		return 11
	}
	file := os.NewFile(uintptr(handle), endpoint)
	defer file.Close()
	// Stay alive until the other side closes, so PID/SID inspection sees the
	// actual live peer. The fixture never sends application or key bytes.
	file.SetReadDeadline(time.Now().Add(10 * time.Second))
	var one [1]byte
	if n, _ := file.Read(one[:]); n != 0 {
		return 8
	}
	return 0
}

func TestNativeWindowsPipePeersRejectOtherSID(t *testing.T) {
	token := nativeOtherWindowsUserToken(t)
	other, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	expectedSID := other.User.Sid.String()
	public, err := windows.KnownFolderPath(windows.FOLDERID_Public, 0)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(public, "devtools-native-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error("cannot remove owned peer fixture", err)
		}
	})
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "peer-fixture.exe")
	if err := os.WriteFile(binary, body, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child := func(t *testing.T, mode, endpoint string) windows.Handle {
		t.Helper()
		identity, err := nativePeerTestPipe(endpoint + "-identity")
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if identity != 0 {
				windows.CloseHandle(identity)
			}
		}()
		process, err := startNativeWindowsPeer(token, binary, directory, mode, endpoint)
		if err != nil {
			t.Fatal("cannot start different-SID fixture", err)
		}
		t.Cleanup(func() {
			windows.TerminateProcess(process, 1)
			windows.WaitForSingleObject(process, 5000)
			windows.CloseHandle(process)
		})
		var processToken windows.Token
		if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &processToken); err != nil {
			t.Fatal("cannot inspect created process token", err)
		}
		actual, err := processToken.GetTokenUser()
		processToken.Close()
		if err != nil || actual.User.Sid.String() != expectedSID {
			t.Fatal("created process has unexpected SID", err)
		}
		ready, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := connectSessionPipe(ready, identity); err != nil {
			var exit uint32
			windows.GetExitCodeProcess(process, &exit)
			t.Fatalf("different-SID readiness did not connect (exit_code=%d): %v", exit, err)
		}
		// Transfer handle ownership to os.File and use an overlapped pipe with
		// a real deadline, rather than an unbounded inherited stdout read.
		output := os.NewFile(uintptr(identity), endpoint+"-identity")
		identity = 0
		defer output.Close()
		if err := output.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(io.LimitReader(output, 256)).ReadString('\n')
		if err != nil || line != expectedSID+"\n" {
			t.Fatal("fixture process did not use the alternate SID", err)
		}
		return process
	}
	wait := func(t *testing.T, process windows.Handle) {
		t.Helper()
		result, err := windows.WaitForSingleObject(process, 10000)
		if err != nil || result != windows.WAIT_OBJECT_0 {
			t.Fatal("different-SID fixture did not finish", err)
		}
		var code uint32
		if err := windows.GetExitCodeProcess(process, &code); err != nil || code != 0 {
			t.Fatal("different-SID fixture failed", err)
		}
	}
	endpoint := func() string {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		return `\\.\pipe\devtools-vault-peer-` + id
	}
	t.Run("runner transport client rejects other-user server", func(t *testing.T) {
		root := privateTempDir(t)
		runner, instance := "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
		listener, err := ListenPeer(ctx, root, runner, instance)
		if err != nil {
			t.Fatal(err)
		}
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		name, err := PeerAddress(root, runner, instance)
		if err != nil {
			t.Fatal(err)
		}
		child(t, "server", name)
		connection, err := DialPeer(ctx, root, runner, instance)
		if connection != nil {
			connection.Close()
		}
		if !errors.Is(err, ErrDenied) {
			t.Fatal("runner client accepted foreign server SID", err)
		}
	})
	t.Run("runner transport private ACL rejects other-user client", func(t *testing.T) {
		root := privateTempDir(t)
		runner, instance := "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
		listener, err := ListenPeer(ctx, root, runner, instance)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		name, err := PeerAddress(root, runner, instance)
		if err != nil {
			t.Fatal(err)
		}
		process := child(t, "denied-client", name)
		wait(t, process)
	})
	t.Run("client rejects other-user server", func(t *testing.T) {
		name := endpoint()
		child(t, "server", name)
		connection, err := dialSession(ctx, name)
		if connection != nil {
			connection.Close()
		}
		if !errors.Is(err, ErrDenied) {
			t.Fatal("client did not reject the foreign server SID", err)
		}
	})
	t.Run("server rejects other-user client", func(t *testing.T) {
		name := endpoint()
		handle, err := nativePeerTestPipe(name)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(handle)
		process := child(t, "client", name)
		if err := connectSessionPipe(ctx, handle); err != nil {
			t.Fatal(err)
		}
		if err := verifySessionPipePeer(handle, false); !errors.Is(err, ErrDenied) {
			t.Fatal("server did not reject the foreign client SID", err)
		}
		// Close the application fixture stream and require successful child exit.
		windows.DisconnectNamedPipe(handle)
		wait(t, process)
	})
	t.Run("private pipe ACL rejects other-user client", func(t *testing.T) {
		name := endpoint()
		handle, err := newSessionPipe(name, true)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(handle)
		process := child(t, "denied-client", name)
		wait(t, process)
	})
	t.Run("remote UNC clients are rejected", func(t *testing.T) {
		owner, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			t.Fatal(err)
		}
		// Same user/SYSTEM ACL as production: only the remote-rejection mode
		// differs. A successful control establishes actual SMB reachability.
		controlName := endpoint()
		control, err := nativePeerPipe(controlName, "D:P(A;;GA;;;"+owner.User.Sid.String()+")(A;;GA;;;SY)", false)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(control)
		openRemote := func(name string) (windows.Handle, error) {
			unc := `\\127.0.0.1\pipe\` + strings.TrimPrefix(name, `\\.\pipe\`)
			ptr, err := windows.UTF16PtrFromString(unc)
			if err != nil {
				return 0, err
			}
			return windows.CreateFile(ptr, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		}
		remote, err := openRemote(controlName)
		if err != nil {
			t.Fatal("actual loopback SMB control is unavailable", err)
		}
		windows.CloseHandle(remote)
		productionName := endpoint()
		production, err := newSessionPipe(productionName, true)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(production)
		remote, err = openRemote(productionName)
		if err == nil {
			windows.CloseHandle(remote)
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatal("production pipe did not reject the reachable remote transport", err)
		}
	})
}

// Report only a public SID over an owned, bounded fixture channel. No inherited
// standard handles are needed by CreateProcessWithTokenW.
func reportWindowsPeerSID(endpoint, sid string) int {
	name, _ := windows.UTF16PtrFromString(endpoint + "-identity")
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return 12
	}
	output := os.NewFile(uintptr(handle), endpoint+"-identity")
	defer output.Close()
	if err := output.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 13
	}
	if err := writeAll(output, []byte(sid+"\n")); err != nil {
		return 14
	}
	return 0
}

// This test-only launcher requires impersonation, not primary-token assignment.
// Keep the original owned process handle; never recover its identity from a PID.
func startNativeWindowsPeer(token windows.Token, binary, directory, mode, endpoint string) (windows.Handle, error) {
	application, _ := windows.UTF16PtrFromString(binary)
	command, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{binary, "__vault-peer-fixture", mode, endpoint}))
	cwd, _ := windows.UTF16PtrFromString(directory)
	environment := utf16.Encode([]rune(strings.Join([]string{"SystemRoot=" + os.Getenv("SystemRoot"), "WINDIR=" + os.Getenv("WINDIR"), ""}, "\x00") + "\x00"))
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var info windows.ProcessInformation
	ok, _, callErr := windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateProcessWithTokenW").Call(uintptr(token), 0, uintptr(unsafe.Pointer(application)), uintptr(unsafe.Pointer(command)), windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW, uintptr(unsafe.Pointer(&environment[0])), uintptr(unsafe.Pointer(cwd)), uintptr(unsafe.Pointer(&startup)), uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(application)
	runtime.KeepAlive(command)
	runtime.KeepAlive(cwd)
	runtime.KeepAlive(environment)
	if ok == 0 {
		return 0, callErr
	}
	windows.CloseHandle(info.Thread)
	return info.Process, nil
}
