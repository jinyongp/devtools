package vault

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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
	child := func(mode, endpoint string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, "__vault-peer-fixture", mode, endpoint)
		cmd.Dir = directory
		cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "WINDIR=" + os.Getenv("WINDIR")}
		cmd.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(token)}
		return cmd
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
		cmd := child("server", name)
		ready, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal("cannot start different-SID server", err)
		}
		defer func() { cmd.Process.Kill(); cmd.Wait() }()
		line, err := bufio.NewReader(ready).ReadString('\n')
		if err != nil || line != expectedSID+"\n" {
			t.Fatal("server process did not use the alternate SID")
		}
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
		cmd := child("client", name)
		var identity bytes.Buffer
		cmd.Stdout = &identity
		if err := cmd.Start(); err != nil {
			t.Fatal("cannot start different-SID client", err)
		}
		defer func() { cmd.Process.Kill(); cmd.Wait() }()
		if err := connectSessionPipe(ctx, handle); err != nil {
			t.Fatal(err)
		}
		if err := verifySessionPipePeer(handle, false); !errors.Is(err, ErrDenied) {
			t.Fatal("server did not reject the foreign client SID", err)
		}
		// Complete the client's read before asserting its reported process SID.
		windows.DisconnectNamedPipe(handle)
		if err := cmd.Wait(); err != nil || identity.String() != expectedSID+"\n" {
			t.Fatal("client process did not use the alternate SID", err)
		}
	})
	t.Run("private pipe ACL rejects other-user client", func(t *testing.T) {
		name := endpoint()
		handle, err := newSessionPipe(name, true)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(handle)
		cmd := child("denied-client", name)
		var identity bytes.Buffer
		cmd.Stdout = &identity
		if err := cmd.Run(); err != nil || identity.String() != expectedSID+"\n" {
			t.Fatal("production pipe ACL did not deny the different-SID process", err)
		}
	})
}
