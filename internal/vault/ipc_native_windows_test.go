package vault

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;WD)")
	if err != nil {
		return 0, err
	}
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	return windows.CreateNamedPipe(ptr, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
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
		fmt.Println(user.User.Sid.String())
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
			fmt.Println(user.User.Sid.String())
			return 0
		}
		if err != nil {
			return 6
		}
		fmt.Println(user.User.Sid.String())
	default:
		return 7
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
	child := func(mode, endpoint string) (*os.Process, *bufio.Reader) {
		t.Helper()
		process, output, err := startNativeWindowsPeer(token, binary, directory, mode, endpoint)
		if err != nil {
			t.Fatal("cannot start different-SID fixture", err)
		}
		t.Cleanup(func() { process.Kill(); process.Wait(); output.Close() })
		go func() {
			<-ctx.Done()
			process.Kill()
		}()
		return process, bufio.NewReader(output)
	}
	identity := func(output *bufio.Reader) {
		t.Helper()
		line, err := output.ReadString('\n')
		if err != nil || line != expectedSID+"\n" {
			t.Fatal("fixture process did not use the alternate SID")
		}
	}
	endpoint := func() string {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		return `\\.\pipe\devtools-vault-peer-` + id
	}
	t.Run("client rejects other-user server", func(t *testing.T) {
		name := endpoint()
		_, output := child("server", name)
		identity(output)
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
		process, output := child("client", name)
		if err := connectSessionPipe(ctx, handle); err != nil {
			t.Fatal(err)
		}
		if err := verifySessionPipePeer(handle, false); !errors.Is(err, ErrDenied) {
			t.Fatal("server did not reject the foreign client SID", err)
		}
		// Complete the client's read before asserting its reported process SID.
		windows.DisconnectNamedPipe(handle)
		identity(output)
		if state, err := process.Wait(); err != nil || !state.Success() {
			t.Fatal("different-SID client did not complete", err)
		}
	})
	t.Run("private pipe ACL rejects other-user client", func(t *testing.T) {
		name := endpoint()
		handle, err := newSessionPipe(name, true)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(handle)
		process, output := child("denied-client", name)
		identity(output)
		if state, err := process.Wait(); err != nil || !state.Success() {
			t.Fatal("production pipe ACL did not deny the different-SID process", err)
		}
	})
}

// CreateProcessWithTokenW uses the runner's impersonation privilege instead of
// CreateProcessAsUser's primary-token assignment privilege. Only public fixture
// code and endpoint names enter the child; no credential environment is inherited.
func startNativeWindowsPeer(token windows.Token, binary, directory, mode, endpoint string) (*os.Process, *os.File, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer write.Close()
	fail := func(err error) (*os.Process, *os.File, error) { read.Close(); return nil, nil, err }
	if err := windows.SetHandleInformation(windows.Handle(write.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return fail(err)
	}
	nullName, _ := windows.UTF16PtrFromString("NUL")
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	null, err := windows.CreateFile(nullName, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, &attrs, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return fail(err)
	}
	defer windows.CloseHandle(null)
	application, _ := windows.UTF16PtrFromString(binary)
	command, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{binary, "__vault-peer-fixture", mode, endpoint}))
	cwd, _ := windows.UTF16PtrFromString(directory)
	environment := utf16.Encode([]rune(strings.Join([]string{"SystemRoot=" + os.Getenv("SystemRoot"), "WINDIR=" + os.Getenv("WINDIR"), ""}, "\x00") + "\x00"))
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{})), Flags: windows.STARTF_USESTDHANDLES, StdInput: null, StdOutput: windows.Handle(write.Fd()), StdErr: null}
	var info windows.ProcessInformation
	ok, _, callErr := windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateProcessWithTokenW").Call(uintptr(token), 0, uintptr(unsafe.Pointer(application)), uintptr(unsafe.Pointer(command)), windows.CREATE_UNICODE_ENVIRONMENT, uintptr(unsafe.Pointer(&environment[0])), uintptr(unsafe.Pointer(cwd)), uintptr(unsafe.Pointer(&startup)), uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(application)
	runtime.KeepAlive(command)
	runtime.KeepAlive(cwd)
	runtime.KeepAlive(environment)
	runtime.KeepAlive(write)
	if ok == 0 {
		return fail(callErr)
	}
	defer windows.CloseHandle(info.Thread)
	defer windows.CloseHandle(info.Process)
	process, err := os.FindProcess(int(info.ProcessId))
	if err != nil {
		windows.TerminateProcess(info.Process, 1)
		return fail(err)
	}
	return process, read, nil
}
