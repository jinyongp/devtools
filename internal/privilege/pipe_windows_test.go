package privilege

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "__windows-network-helper" {
		err := ServePipe(context.Background(), os.Args[2], func(ctx context.Context, kind string, body json.RawMessage) *protocol.Error {
			var input struct {
				Value string `json:"value"`
			}
			if strictHelperJSON(body, &input) != nil {
				return protocol.NewError("invalid_argument", "Invalid fixture body.", 2, nil)
			}
			if input.Value == "reject" {
				return protocol.NewError("request_conflict", "Explicit fixture rejection.", 3, nil)
			}
			if input.Value == "wait" {
				<-ctx.Done()
				return helperCanceled(ctx)
			}
			if kind != "trust" || input.Value != "한글 🧪" {
				return protocol.NewError("invalid_argument", "Unexpected fixture request.", 2, nil)
			}
			return nil
		})
		if err != nil {
			os.Exit(90)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "__windows-helper-sleep" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestWindowsConstrainedPipeRoundtripAndOperationError(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if failure := runPlatform(ctx, "trust", []byte(`{"value":"한글 🧪"}`), false, exe); failure != nil {
		t.Fatalf("authenticated one-shot helper: %v", failure)
	}
	if failure := runPlatform(ctx, "trust", []byte(`{"value":"reject"}`), false, exe); failure == nil || failure.Code != "request_conflict" {
		t.Fatalf("operation error lost: %v", failure)
	}
}
func TestWindowsConstrainedHelperCancellationAndBounds(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if failure := runPlatform(canceled, "trust", []byte(`{}`), false, exe); failure == nil || failure.Code != "canceled" {
		t.Fatal(failure)
	}
	if failure := runPlatform(context.Background(), "trust", make([]byte, helperLimit), false, exe); failure == nil || failure.Code != "invalid_argument" {
		t.Fatal(failure)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if failure := runPlatform(ctx, "trust", []byte(`{"value":"wait"}`), false, exe); failure == nil {
		t.Fatal("canceled helper succeeded")
	}
}
func TestWindowsConstrainedHelperImagePinsRejectRenameAndForeignProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	guard, identity, err := openHelperImage(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err = verifyHelperProcess(windows.CurrentProcess(), identity); err != nil {
		t.Fatal(err)
	}
	// The live image can be read, but cannot be substituted during authorization.
	if err = os.Rename(exe, exe+".unexpected"); err == nil {
		os.Rename(exe+".unexpected", exe)
		t.Fatal("held helper image was renamed")
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(system, "ping.exe"), "-n", "10", "127.0.0.1")
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err = verifyHelperProcess(handle, identity); err == nil {
		t.Fatal("foreign process image authenticated")
	}
}
func TestWindowsConstrainedHelperPipeNamesAndShellABI(t *testing.T) {
	if unsafe.Sizeof(shellExecuteInfo{}) != 112 {
		t.Fatalf("ShellExecuteEx ABI size=%d", unsafe.Sizeof(shellExecuteInfo{}))
	}
	for _, name := range []string{"", `\\server\pipe\devtools-helper-1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, helperPrefix + "0-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", helperPrefix + "1-../../outside", helperPrefix + "1-gggggggggggggggggggggggggggggggg"} {
		if _, err := helperPipeParent(name); err == nil {
			t.Fatal("invalid helper pipe accepted")
		}
	}
	if _, err := helperPipeParent(helperPrefix + "1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := strictHelperJSON([]byte(`{"version":1,"command":"arbitrary"}`), &helperRequest{}); err == nil {
		t.Fatal("unknown helper field accepted")
	}
	if err := strictHelperJSON([]byte(`{} {}`), &helperRequest{}); err == nil {
		t.Fatal("second helper value accepted")
	}
}
func TestWindowsConstrainedPipeMissingEndpointDoesNotFallback(t *testing.T) {
	// An invalid pipe name never becomes a file target or network fallback.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if file, err := clientHelperPipe(ctx, helperPrefix+"1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		file.Close()
		t.Fatal("missing pipe unexpectedly connected")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("missing pipe was retried as busy")
	}
}

func TestWindowsConstrainedPipeClientUsesIdentificationToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name := helperPrefix + strconv.Itoa(os.Getpid()) + "-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	ptr, _ := windows.UTF16PtrFromString(name)
	pipe, err := windows.CreateNamedPipe(ptr, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED, windows.PIPE_TYPE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 4096, 4096, 0, &attributes)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(pipe)
	type opened struct {
		file *os.File
		err  error
	}
	done := make(chan opened, 1)
	go func() { file, err := clientHelperPipe(ctx, name); done <- opened{file, err} }()
	if err = connectHelperPipe(ctx, pipe); err != nil {
		t.Fatal(err)
	}
	client := <-done
	if client.err != nil {
		t.Fatal(client.err)
	}
	defer client.file.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ok, _, err := windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient").Call(uintptr(pipe))
	if ok == 0 {
		t.Fatal(err)
	}
	defer windows.RevertToSelf()
	var token windows.Token
	if err = windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &token); err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	var level uint32
	var size uint32
	if err = windows.GetTokenInformation(token, windows.TokenImpersonationLevel, (*byte)(unsafe.Pointer(&level)), uint32(unsafe.Sizeof(level)), &size); err != nil {
		t.Fatal(err)
	}
	if level != windows.SecurityIdentification {
		t.Fatalf("pipe exposed impersonation-capable token: level=%d", level)
	}
}

func TestWindowsConstrainedPipeRejectsWrongPIDWithSameImage(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	guard, identity, err := openHelperImage(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	child := exec.Command(exe, "__windows-helper-sleep")
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(child.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	if err = verifyHelperProcess(process, identity); err != nil {
		t.Fatal(err)
	}
	name := helperPrefix + strconv.Itoa(os.Getpid()) + "-cccccccccccccccccccccccccccccccc"
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	ptr, _ := windows.UTF16PtrFromString(name)
	pipe, err := windows.CreateNamedPipe(ptr, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED, windows.PIPE_TYPE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 4096, 4096, 0, &attributes)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(pipe)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	connected := make(chan error, 1)
	go func() {
		file, err := clientHelperPipe(ctx, name)
		if err == nil {
			defer file.Close()
			var b [1]byte
			_, _ = file.Read(b[:])
		}
		connected <- err
	}()
	if err = acceptHelperPipe(ctx, pipe, process, identity); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrong PID authenticated or failed unexpectedly: %v", err)
	}
	if err = <-connected; err != nil {
		t.Fatal("wrong-PID fixture did not actually connect", err)
	}
}
