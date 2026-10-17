package expose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixturePipeListener struct {
	path    string
	mu      sync.Mutex
	pending windows.Handle
	closed  bool
	ctx     context.Context
	cancel  context.CancelFunc
}

func newFixturePipeHandle(path string, count uint32) (windows.Handle, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateNamedPipe(pointer, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, count, 65536, 65536, 0, nil)
}
func newFixturePipe(t *testing.T) *fixturePipeListener {
	t.Helper()
	path := fmt.Sprintf(`\\.\pipe\ProtectedPrefix\Administrators\devtools-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	h, err := newFixturePipeHandle(path, 255)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &fixturePipeListener{path: path, pending: h, ctx: ctx, cancel: cancel}
	t.Cleanup(func() { l.Close() })
	return l
}
func (l *fixturePipeListener) Addr() net.Addr { return pipeAddress(l.path) }
func (l *fixturePipeListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		l.cancel()
		if l.pending != 0 {
			windows.CloseHandle(l.pending)
			l.pending = 0
		}
	}
	return nil
}
func (l *fixturePipeListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, net.ErrClosed
	}
	h := l.pending
	l.pending = 0
	l.mu.Unlock()
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	defer windows.CloseHandle(event)
	ov := windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(h, &ov)
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		for {
			status, e := windows.WaitForSingleObject(event, 20)
			if e != nil || l.ctx.Err() != nil {
				windows.CancelIoEx(h, &ov)
				var transferred uint32
				windows.GetOverlappedResult(h, &ov, &transferred, true)
				windows.CloseHandle(h)
				return nil, net.ErrClosed
			}
			if status == windows.WAIT_OBJECT_0 {
				var transferred uint32
				err = windows.GetOverlappedResult(h, &ov, &transferred, false)
				break
			}
		}
	}
	if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		windows.CloseHandle(h)
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		windows.CloseHandle(h)
		return nil, net.ErrClosed
	}
	l.pending, err = newFixturePipeHandle(l.path, 255)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &tailscalePipeConn{File: os.NewFile(uintptr(h), l.path), address: pipeAddress(l.path)}, nil
}
func pipeHTTPFixture(t *testing.T, handler http.HandlerFunc) tailscaleLocalAPI {
	t.Helper()
	l := newFixturePipe(t)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(l) }()
	t.Cleanup(func() { server.Close(); l.Close(); <-done })
	return windowsTailscaleAPI(l.path)
}
func TestWindowsTailscaleActualPipeETagAndFutureFields(t *testing.T) {
	var requests atomic.Int32
	api := pipeHTTPFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/localapi/v0/serve-config" || r.Host != "local-tailscaled.sock" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected local API request")
		}
		if r.Method == "GET" {
			w.Header().Set("ETag", `"v1"`)
			io.WriteString(w, `{"Future":{"keep":true}}`)
			return
		}
		if r.Header.Get("If-Match") != `"v1"` {
			w.WriteHeader(412)
			return
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil || string(body["Future"]) != `{"keep":true}` {
			t.Error("future configuration lost")
		}
		io.WriteString(w, `{}`)
	})
	config, etag, failure := api.Read(context.Background())
	if failure != nil {
		t.Fatal(failure)
	}
	if failure = changeServeHandler(config, "fixture.ts.net:443", 443, "http://127.0.0.1:31000", false); failure != nil {
		t.Fatal(failure)
	}
	if failure = api.Write(context.Background(), config, etag); failure != nil {
		t.Fatal(failure)
	}
	if failure = api.Write(context.Background(), config, `"stale"`); failure == nil || failure.Code != "tailscale_configuration_changed" {
		t.Fatal(failure)
	}
	if requests.Load() != 3 {
		t.Fatal("unexpected retries", requests.Load())
	}
}
func TestWindowsTailscalePipeRefusalsAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status           int
		body, etag, code string
	}{
		{"unauthorized", 401, "", "", "tailscale_permission_required"},
		{"forbidden", 403, "", "", "tailscale_permission_required"},
		{"redirect", 302, "", "", "tailscale_command_failed"},
		{"oversized", 200, strings.Repeat("x", (1<<20)+1), "v1", "tailscale_status_invalid"},
		{"no-etag", 200, `{}`, "", "tailscale_atomic_config_unavailable"},
		{"invalid-json", 200, `{`, "v1", "tailscale_status_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			api := pipeHTTPFixture(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("ETag", tc.etag)
				w.Header().Set("Location", "http://example.invalid/never")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, _, failure := api.Read(context.Background())
			if failure == nil || failure.Code != tc.code {
				t.Fatal(failure)
			}
			if requests.Load() != 1 {
				t.Fatal("unexpected redirect or retry")
			}
		})
	}
}
func TestWindowsTailscalePipeCancellationAndDeadline(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			entered := make(chan struct{})
			departed := make(chan struct{})
			api := pipeHTTPFixture(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(departed) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
				defer cancel()
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _, failure := api.Read(ctx)
				if failure == nil {
					t.Error("canceled request succeeded")
				}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("request never connected")
			}
			if !deadline {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("request cancellation blocked")
			}
			select {
			case <-departed:
			case <-time.After(2 * time.Second):
				t.Fatal("canceled transport retained pipe")
			}
		})
	}
}
func TestWindowsTailscalePipeMissingBusyAndNamespace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c, err := dialTailscalePipe(ctx, `\\.\pipe\unprotected`); err == nil {
		c.Close()
		t.Fatal("unprotected endpoint accepted")
	}
	missing := fmt.Sprintf(`\\.\pipe\ProtectedPrefix\Administrators\missing-%d`, time.Now().UnixNano())
	if c, err := dialTailscalePipe(ctx, missing); err == nil {
		c.Close()
		t.Fatal("missing endpoint accepted")
	}
	path := fmt.Sprintf(`\\.\pipe\ProtectedPrefix\Administrators\busy-%d`, time.Now().UnixNano())
	h, err := newFixturePipeHandle(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	c, err := dialTailscalePipe(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	busy, stop := context.WithTimeout(ctx, 60*time.Millisecond)
	defer stop()
	if next, err := dialTailscalePipe(busy, path); !errors.Is(err, context.DeadlineExceeded) {
		if next != nil {
			next.Close()
		}
		t.Fatal("busy pipe did not respect deadline", err)
	}
}
func TestWindowsTailscaleVersionAndNoCredentialProxy(t *testing.T) {
	for _, version := range []string{"1.104.1", "1.104.2", "1.104.1-test", "2.0.0"} {
		if !platformTailscaleVersion(version) {
			t.Fatal(version)
		}
	}
	for _, version := range []string{"1.104.0", "1.103.9", "1.86.0", "", "1.104", "a.b.c"} {
		if platformTailscaleVersion(version) {
			t.Fatal(version)
		}
	}
	p := Tailscale{Run: func(context.Context, []string) ([]byte, *protocol.Error) {
		t.Fatal("Windows config invoked credential proxy")
		return nil, nil
	}}
	api, failure := p.configAPI(context.Background())
	if failure != nil || api == nil {
		t.Fatal(failure)
	}
}
