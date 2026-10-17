package expose

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

const tailscalePipe = `\\.\pipe\ProtectedPrefix\Administrators\Tailscale\tailscaled`

func platformTailscaleVersion(value string) bool {
	base, _, _ := strings.Cut(value, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return false
	}
	numbers := [3]int{}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return false
		}
		numbers[i] = n
	}
	return numbers[0] > 1 || numbers[0] == 1 && (numbers[1] > 104 || numbers[1] == 104 && numbers[2] >= 1)
}
func (p Tailscale) platformConfigAPI(context.Context) (tailscaleConfigAPI, *protocol.Error) {
	// Never call debug local-creds on Windows: it launches a persistent proxy.
	return windowsTailscaleAPI(tailscalePipe), nil
}
func windowsTailscaleAPI(path string) tailscaleLocalAPI {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dialTailscalePipe(ctx, path) }}
	return tailscaleLocalAPI{client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type pipeAddress string

func (a pipeAddress) Network() string { return "npipe" }
func (a pipeAddress) String() string  { return string(a) }

type tailscalePipeConn struct {
	*os.File
	address pipeAddress
}

func (c *tailscalePipeConn) LocalAddr() net.Addr  { return c.address }
func (c *tailscalePipeConn) RemoteAddr() net.Addr { return c.address }

func dialTailscalePipe(ctx context.Context, path string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Only a protected namespace is accepted, including injected native fixtures.
	const prefix = `\\.\pipe\ProtectedPrefix\Administrators\`
	if !strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)) {
		return nil, errors.New("protected Tailscale pipe required")
	}
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		handle, err := windows.CreateFile(pointer, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if err == nil {
			kind, err := windows.GetFileType(handle)
			if err != nil || kind != windows.FILE_TYPE_PIPE {
				windows.CloseHandle(handle)
				return nil, errors.New("named pipe required")
			}
			file := os.NewFile(uintptr(handle), path)
			if deadline, ok := ctx.Deadline(); ok {
				if err = file.SetDeadline(deadline); err != nil {
					file.Close()
					return nil, err
				}
			}
			// HTTP Transport owns the returned connection and closes it on request
			// cancellation. DialContext may end immediately after dialing succeeds.
			return &tailscalePipeConn{File: file, address: pipeAddress(path)}, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, err
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
