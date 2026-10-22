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

type windowsSessionConnection struct{ *os.File }

var peekSessionPipe = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

func (c *windowsSessionConnection) waitForInput(ctx context.Context) {
	// Go's Windows os.File currently updates a shared offset on both pipe
	// Read and Write. Observe disconnects/extra input without a concurrent Read.
	// These handles use FILE_FLAG_OVERLAPPED, so PeekNamedPipe cannot wait for
	// a concurrent synchronous operation to complete.
	// https://learn.microsoft.com/windows/win32/api/namedpipeapi/nf-namedpipeapi-peeknamedpipe
	raw, err := c.SyscallConn()
	if err != nil {
		return
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var available uint32
		var failure error
		err := raw.Control(func(handle uintptr) {
			ok, _, err := peekSessionPipe.Call(handle, 0, 0, 0, uintptr(unsafe.Pointer(&available)), 0)
			if ok == 0 {
				failure = err
			}
		})
		if err != nil || failure != nil || available != 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		if err := l.ctx.Err(); err != nil {
			return nil, err
		}
		h := l.initial
		l.initial = 0
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
		// Keep a server instance alive before handing this connection to its
		// worker. Closing the last instance removes the named endpoint, so a
		// fast following client could otherwise see ERROR_FILE_NOT_FOUND
		// before the accept loop creates its next pipe.
		next, err := newSessionPipe(l.name, false)
		if err == nil {
			err = l.ctx.Err()
		}
		if err == nil {
			l.initial = next
		} else if next != 0 {
			windows.CloseHandle(next)
		}
		if err != nil {
			windows.CloseHandle(h)
			return nil, err
		}
		f := os.NewFile(uintptr(h), l.name)
		if err := verifySessionPipePeer(h, false); err != nil {
			f.Close()
			continue
		}
		return &windowsSessionConnection{File: f}, nil
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
