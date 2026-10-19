package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func sessionEndpoint(directory, id string) string {
	digest := sha256.Sum256([]byte(directory))
	return `\\.\pipe\devtools-vault-` + hex.EncodeToString(digest[:16]) + "-" + id
}

type windowsSessionListener struct {
	mu      sync.Mutex
	name    string
	initial windows.Handle
	ctx     context.Context
	cancel  context.CancelFunc
}

func newSessionPipe(name string, first bool) (windows.Handle, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return 0, ErrDenied
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)")
	if err != nil {
		return 0, ErrDenied
	}
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, ErrInvalid
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if first {
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	h, err := windows.CreateNamedPipe(p, flags, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 16, 64<<10, 64<<10, 0, &attrs)
	if err != nil {
		return 0, ErrUnavailable
	}
	return h, nil
}
func listenSession(directory, id string) (sessionListener, error) {
	name := sessionEndpoint(directory, id)
	h, err := newSessionPipe(name, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &windowsSessionListener{name: name, initial: h, ctx: ctx, cancel: cancel}, nil
}
func (l *windowsSessionListener) Close() error {
	l.cancel()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.initial != 0 {
		windows.CloseHandle(l.initial)
		l.initial = 0
	}
	return nil
}
func (l *windowsSessionListener) Accept(ctx context.Context) (sessionConnection, error) {
	stop := context.AfterFunc(ctx, l.cancel)
	defer stop()
	for {
		if err := l.ctx.Err(); err != nil {
			return nil, err
		}
		l.mu.Lock()
		h := l.initial
		l.initial = 0
		l.mu.Unlock()
		if h == 0 {
			var err error
			h, err = newSessionPipe(l.name, false)
			if err != nil {
				return nil, err
			}
		}
		if err := connectSessionPipe(l.ctx, h); err != nil {
			windows.CloseHandle(h)
			return nil, err
		}
		f := os.NewFile(uintptr(h), l.name)
		if err := verifySessionPipePeer(h, false); err != nil {
			f.Close()
			continue
		}
		return f, nil
	}
}
func connectSessionPipe(ctx context.Context, h windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	ov := windows.Overlapped{HEvent: event}
	var pin runtime.Pinner
	pin.Pin(&ov)
	defer pin.Unpin()
	err = windows.ConnectNamedPipe(h, &ov)
	if err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return err
	}
	defer func() { windows.CancelIoEx(h, &ov); var n uint32; windows.GetOverlappedResult(h, &ov, &n, true) }()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		result, err := windows.WaitForSingleObject(event, 10)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			var n uint32
			return windows.GetOverlappedResult(h, &ov, &n, false)
		}
	}
}
func verifySessionPipePeer(h windows.Handle, server bool) error {
	var pid uint32
	var err error
	if server {
		err = windows.GetNamedPipeServerProcessId(h, &pid)
	} else {
		err = windows.GetNamedPipeClientProcessId(h, &pid)
	}
	if err != nil {
		return ErrDenied
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return ErrDenied
	}
	defer windows.CloseHandle(p)
	if state, err := windows.WaitForSingleObject(p, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		return ErrDenied
	}
	var token windows.Token
	if err = windows.OpenProcessToken(p, windows.TOKEN_QUERY, &token); err != nil {
		return ErrDenied
	}
	defer token.Close()
	peer, err := token.GetTokenUser()
	if err != nil {
		return ErrDenied
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || !windows.EqualSid(peer.User.Sid, user.User.Sid) {
		return ErrDenied
	}
	return nil
}
func dialSession(ctx context.Context, endpoint string) (sessionConnection, error) {
	p, err := windows.UTF16PtrFromString(endpoint)
	if err != nil {
		return nil, ErrInvalid
	}
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		h, failure := windows.CreateFile(p, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if failure == nil {
			f := os.NewFile(uintptr(h), endpoint)
			if err = verifySessionPipePeer(h, true); err != nil {
				f.Close()
				return nil, err
			}
			return f, nil
		}
		if !errors.Is(failure, windows.ERROR_PIPE_BUSY) {
			return nil, ErrUnavailable
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
