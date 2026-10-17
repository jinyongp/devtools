package proxy

import (
	"golang.org/x/sys/windows"
	"net"
	"syscall"
	"testing"
)

func TestWindowsListenerBindDiagnostics(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{{windows.WSAEACCES, "permission_required"}, {windows.WSAEADDRINUSE, "port_in_use"}, {windows.WSAEINVAL, "proxy_start_failed"}} {
		wrapped := &net.OpError{Op: "listen", Net: "tcp", Err: test.err}
		failure := listenerBindFailure(wrapped)
		if failure == nil || failure.Code != test.code {
			t.Fatalf("bind diagnostic: %v", failure)
		}
	}
}
func TestWindowsLoopbackListenerUsesExclusiveBindings(t *testing.T) {
	listeners, err := ListenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	for _, l := range listeners {
		raw, err := l.(*net.TCPListener).SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var option int
		var optionErr error
		if err = raw.Control(func(fd uintptr) {
			option, optionErr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, int(^int32(syscall.SO_REUSEADDR)))
		}); err != nil {
			t.Fatal(err)
		}
		if optionErr != nil || option != 1 {
			t.Fatalf("proxy listener lacks kernel exclusive bind: value=%d error=%v", option, optionErr)
		}
	}
}
