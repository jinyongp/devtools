package privilege

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/winimage"
	"golang.org/x/sys/windows"
)

const helperLimit = 1 << 20
const helperPrefix = `\\.\pipe\devtools-helper-`

type helperRequest struct {
	Version int             `json:"version"`
	Kind    string          `json:"kind"`
	Body    json.RawMessage `json:"body"`
}
type helperResponse struct {
	Version int             `json:"version"`
	Error   *protocol.Error `json:"error,omitempty"`
}
type HelperOperation func(context.Context, string, json.RawMessage) *protocol.Error

func helperFailure() *protocol.Error {
	return protocol.NewError("permission_required", "The constrained Windows helper did not complete. Interactive hosts changes require same-user UAC approval; noninteractive changes require an already elevated token. Retry the existing operation to reconcile any recorded result.", 3, nil)
}
func helperCanceled(ctx context.Context) *protocol.Error {
	if ctx.Err() != nil {
		return protocol.NewError("canceled", "Windows helper operation canceled; reconcile the existing operation receipt before retrying.", 130, nil)
	}
	return helperFailure()
}
func strictHelperJSON(body []byte, value any) error {
	reader := json.NewDecoder(strings.NewReader(string(body)))
	reader.DisallowUnknownFields()
	if err := reader.Decode(value); err != nil {
		return err
	}
	if reader.Decode(new(any)) != io.EOF {
		return errors.New("trailing helper input")
	}
	return nil
}
func writeHelperFrame(file *os.File, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(body) > helperLimit {
		return errors.New("helper frame too large")
	}
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(body)))
	if _, err = file.Write(size[:]); err != nil {
		return err
	}
	_, err = file.Write(body)
	return err
}
func readHelperFrame(file *os.File, value any) error {
	var size [4]byte
	if _, err := io.ReadFull(file, size[:]); err != nil {
		return err
	}
	length := binary.LittleEndian.Uint32(size[:])
	if length == 0 || length > helperLimit {
		return errors.New("invalid helper frame size")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(file, body); err != nil {
		return err
	}
	return strictHelperJSON(body, value)
}
func processSID(handle windows.Handle) (*windows.SID, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token); err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}
func verifyHelperProcess(handle windows.Handle, expected winimage.Identity) error {
	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return err
	}
	if result != uint32(windows.WAIT_TIMEOUT) {
		return errors.New("helper peer exited")
	}
	sid, err := processSID(handle)
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if !windows.EqualSid(sid, user.User.Sid) {
		return errors.New("helper peer SID differs")
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err = windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return err
	}
	file, actual, failure := winimage.Open(windows.UTF16ToString(buffer[:size]))
	if failure != nil {
		return failure
	}
	defer file.Close()
	if actual != expected {
		return errors.New("helper peer image identity differs")
	}
	return nil
}
func openHelperImage(path string) (*platformfs.File, winimage.Identity, error) {
	guard, err := platformfs.OpenPublicNoFollow(path, false)
	if err != nil {
		return nil, winimage.Identity{}, err
	}
	if err = platformfs.CheckTrustedImage(guard); err != nil {
		guard.Close()
		return nil, winimage.Identity{}, err
	}
	file, identity, failure := winimage.Open(path)
	if failure != nil {
		guard.Close()
		return nil, winimage.Identity{}, failure
	}
	defer file.Close()
	var actual windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(guard.Fd()), &actual); err != nil {
		guard.Close()
		return nil, winimage.Identity{}, err
	}
	if uint64(actual.VolumeSerialNumber) != identity.Device || uint64(actual.FileIndexHigh)<<32|uint64(actual.FileIndexLow) != identity.Inode {
		guard.Close()
		return nil, winimage.Identity{}, platformfs.ErrUnsafe
	}
	return guard, identity, nil
}
func helperPipeParent(name string) (uint32, error) {
	if !strings.HasPrefix(name, helperPrefix) {
		return 0, errors.New("invalid helper pipe")
	}
	parts := strings.Split(strings.TrimPrefix(name, helperPrefix), "-")
	if len(parts) != 2 || len(parts[1]) != 32 {
		return 0, errors.New("invalid helper pipe")
	}
	for _, r := range parts[1] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return 0, errors.New("invalid helper pipe nonce")
		}
	}
	pid, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil || pid == 0 {
		return 0, errors.New("invalid helper parent")
	}
	return uint32(pid), nil
}
func connectHelperPipe(ctx context.Context, pipe windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	overlapped := windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(pipe, &overlapped)
	if err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return err
	}
	defer func() {
		_ = windows.CancelIoEx(pipe, &overlapped)
		var done uint32
		_ = windows.GetOverlappedResult(pipe, &overlapped, &done, true)
	}()
	for {
		if err = ctx.Err(); err != nil {
			_ = windows.CancelIoEx(pipe, &overlapped)
			var done uint32
			_ = windows.GetOverlappedResult(pipe, &overlapped, &done, true)
			return err
		}
		result, err := windows.WaitForSingleObject(event, 10)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			var done uint32
			return windows.GetOverlappedResult(pipe, &overlapped, &done, false)
		}
	}
}
func clientHelperPipe(ctx context.Context, name string) (*os.File, error) {
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		handle, e := windows.CreateFile(ptr, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if e == nil {
			file := os.NewFile(uintptr(handle), name)
			deadline, _ := ctx.Deadline()
			if err = file.SetDeadline(deadline); err != nil {
				file.Close()
				return nil, err
			}
			return file, nil
		}
		if !errors.Is(e, windows.ERROR_PIPE_BUSY) {
			return nil, e
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// ServePipe authenticates both SID and held image identity before reading a
// one-shot allowlisted request. The pipe name is the only helper argument.
func ServePipe(ctx context.Context, name string, operation HelperOperation) error {
	if os.Getenv("DEVTOOLS_ISOLATED_ROOT") != "" {
		return errors.New("isolated helper")
	}
	parentPID, err := helperPipeParent(name)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	image, identity, err := openHelperImage(exe)
	if err != nil {
		return err
	}
	defer image.Close()
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	parent, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, parentPID)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(parent)
	if err = verifyHelperProcess(parent, identity); err != nil {
		return err
	}
	file, err := clientHelperPipe(ctx, name)
	if err != nil {
		return err
	}
	defer file.Close()
	stop := context.AfterFunc(ctx, func() { file.Close() })
	defer stop()
	var serverPID uint32
	if err = windows.GetNamedPipeServerProcessId(windows.Handle(file.Fd()), &serverPID); err != nil || serverPID != parentPID {
		return errors.New("helper pipe server differs")
	}
	if err = verifyHelperProcess(parent, identity); err != nil {
		return err
	}
	var request helperRequest
	if err = readHelperFrame(file, &request); err != nil {
		return err
	}
	if request.Version != 1 || (request.Kind != "hosts" && request.Kind != "trust" && request.Kind != "netsetup") || len(request.Body) == 0 {
		return errors.New("invalid helper request")
	}
	if request.Kind != "trust" && !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("elevated helper required")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A one-shot peer sends no more input. EOF/cancellation must also cancel
	// work already received, without leaving an unowned blocked pipe reader.
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); var extra [1]byte; _, _ = file.Read(extra[:]); cancel() }()
	defer func() { file.Close(); <-watchDone }()
	failure := operation(ctx, request.Kind, request.Body)
	return writeHelperFrame(file, helperResponse{Version: 1, Error: failure})
}

func acceptHelperPipe(ctx context.Context, pipe, process windows.Handle, identity winimage.Identity) error {
	pid, err := windows.GetProcessId(process)
	if err != nil {
		return err
	}
	for {
		if err = connectHelperPipe(ctx, pipe); err != nil {
			return err
		}
		var client uint32
		if windows.GetNamedPipeClientProcessId(pipe, &client) == nil && client == pid {
			return verifyHelperProcess(process, identity)
		}
		if err = windows.DisconnectNamedPipe(pipe); err != nil {
			return err
		}
	}
}
