package privilege

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

func runPlatform(ctx context.Context, kind string, encoded []byte, terminal bool, executable string) *protocol.Error {
	if ctx.Err() != nil {
		return helperCanceled(ctx)
	}
	if len(encoded) > helperLimit-256 {
		return protocol.NewError("invalid_argument", "Windows helper input exceeds its bounded request size.", 2, nil)
	}
	if kind == "trust" && !terminal {
		return protocol.NewError("permission_required", "Windows CurrentUser trust changes require an interactive terminal and approval of the OS certificate confirmation. Export the public CA when preparing a separate client.", 3, nil)
	}
	elevated := windows.GetCurrentProcessToken().IsElevated()
	if kind != "trust" && !elevated && !terminal {
		return helperFailure()
	}
	image, identity, err := openHelperImage(executable)
	if err != nil {
		return helperFailure()
	}
	defer image.Close()
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return helperFailure()
	}
	name := helperPrefix + strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(random[:])
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return helperFailure()
	}
	// Specific data rights exclude FILE_CREATE_PIPE_INSTANCE from the client ACE.
	descriptor, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;0x12019b;;;" + user.User.Sid.String() + ")(A;;0x12019b;;;SY)")
	if err != nil {
		return helperFailure()
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return helperFailure()
	}
	pipe, err := windows.CreateNamedPipe(ptr, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE, windows.PIPE_TYPE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 65536, 65536, 0, &attributes)
	runtime.KeepAlive(descriptor)
	if err != nil {
		return helperFailure()
	}
	rawOwned := true
	defer func() {
		if rawOwned {
			windows.CloseHandle(pipe)
		}
	}()
	process, err := spawnWindowsHelper(ctx, identity.Path, name, kind != "trust" && !elevated)
	if err != nil {
		return helperCanceled(ctx)
	}
	defer windows.CloseHandle(process)
	if kind == "trust" {
		finished := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { defer close(finished); _ = windows.TerminateProcess(process, 130) })
		defer func() {
			if !stop() {
				<-finished
			}
		}()
	}
	if err = acceptHelperPipe(ctx, pipe, process, identity); err != nil {
		return helperCanceled(ctx)
	}

	file := os.NewFile(uintptr(pipe), name)
	rawOwned = false
	defer file.Close()
	deadline, _ := ctx.Deadline()
	if err = file.SetDeadline(deadline); err != nil {
		return helperFailure()
	}
	stop := context.AfterFunc(ctx, func() { file.Close() })
	defer stop()
	if ctx.Err() != nil {
		return helperCanceled(ctx)
	}
	if err = writeHelperFrame(file, helperRequest{Version: 1, Kind: kind, Body: encoded}); err != nil {
		return helperCanceled(ctx)
	}
	var response helperResponse
	if err = readHelperFrame(file, &response); err != nil || response.Version != 1 {
		return helperCanceled(ctx)
	}
	return response.Error
}

type shellExecuteInfo struct {
	Size, Mask                        uint32
	Window                            windows.Handle
	Verb, File, Parameters, Directory *uint16
	Show                              int32
	Instance                          windows.Handle
	IDList                            unsafe.Pointer
	Class                             *uint16
	ClassKey                          windows.Handle
	HotKey                            uint32
	Icon                              windows.Handle
	Process                           windows.Handle
}

func spawnWindowsHelper(ctx context.Context, executable, pipe string, elevate bool) (windows.Handle, error) {
	if !elevate {
		command := exec.Command(executable, "__windows-network-helper", pipe)
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, HideWindow: true}
		if err := command.Start(); err != nil {
			return 0, err
		}
		// Cmd still holds its creation handle, preventing PID reuse before duplication.
		handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(command.Process.Pid))
		if err != nil {
			command.Process.Kill()
			command.Wait()
			return 0, err
		}
		go command.Wait()
		return handle, nil
	}
	type launch struct {
		handle windows.Handle
		err    error
	}
	result := make(chan launch)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := windows.CoInitializeEx(0, 6) // apartment threaded, OLE1 DDE disabled
		if err != nil && err != syscall.Errno(1) {
			select {
			case result <- launch{err: err}:
			case <-ctx.Done():
			}
			return
		}
		defer windows.CoUninitialize()
		verb, _ := windows.UTF16PtrFromString("runas")
		file, _ := windows.UTF16PtrFromString(executable)
		args, _ := windows.UTF16PtrFromString(syscall.EscapeArg("__windows-network-helper") + " " + syscall.EscapeArg(pipe))
		info := shellExecuteInfo{Size: uint32(unsafe.Sizeof(shellExecuteInfo{})), Mask: 0x40 | 0x100, Verb: verb, File: file, Parameters: args, Show: 0}
		ok, _, callErr := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW").Call(uintptr(unsafe.Pointer(&info)))
		if ok != 0 && info.Process != 0 {
			select {
			case result <- launch{handle: info.Process}:
			case <-ctx.Done():
				windows.CloseHandle(info.Process)
			}
			return
		}
		if info.Process != 0 {
			windows.CloseHandle(info.Process)
		}
		select {
		case result <- launch{err: callErr}:
		case <-ctx.Done():
		}
	}()
	select {
	case value := <-result:
		return value.handle, value.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
