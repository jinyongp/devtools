package ports

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestWindowsExclusiveWildcardAndSpecificBindings(t *testing.T) {
	for _, target := range []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp4", "127.0.0.1"}, {"tcp6", "::"}, {"tcp6", "::1"}} {
		t.Run(target.network+"/"+target.host, func(t *testing.T) {
			first, err := ListenTCP(context.Background(), target.network, net.JoinHostPort(target.host, "0"))
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			port := first.Addr().(*net.TCPAddr).Port
			if free, e := Available(port); e != nil || free {
				t.Fatalf("exclusive owner missed: free=%v error=%v", free, e)
			}
			second, err := ListenTCP(context.Background(), target.network, net.JoinHostPort(target.host, strconv.Itoa(port)))
			if second != nil {
				second.Close()
				t.Fatal("second owner bound exclusive address")
			}
			if !BindAddressInUse(err) && !BindPermissionDenied(err) {
				t.Fatalf("unclassified bind conflict: %v", err)
			}
		})
	}
}
func TestWindowsConcurrentExclusiveBindHasOneOwner(t *testing.T) {
	first, err := ListenTCP(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := first.Addr().String()
	first.Close()
	const count = 16
	start := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	results := make(chan error, count)
	for range count {
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			l, err := ListenTCP(ctx, "tcp4", address)
			results <- err
			if l != nil {
				<-release
				l.Close()
			}
		}()
	}
	close(start)
	owners := 0
	for range count {
		err := <-results
		if err == nil {
			owners++
		} else if !BindAddressInUse(err) && !BindPermissionDenied(err) {
			t.Fatalf("unexpected bind error: %v", err)
		}
	}
	if owners != 1 {
		t.Fatalf("exclusive owners=%d, want exactly one", owners)
	}
}
func TestWindowsActualAccessDeniedBindIsNotFree(t *testing.T) {
	first, err := ListenTCP(context.Background(), "tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var e error
		if err := raw.Control(func(fd uintptr) {
			e = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
		}); err != nil {
			return err
		}
		return e
	}}
	second, err := config.Listen(context.Background(), "tcp4", first.Addr().String())
	if second != nil {
		second.Close()
		t.Fatal("reuse bypassed exclusive owner")
	}
	if !errors.Is(err, windows.WSAEACCES) {
		t.Fatalf("expected real Winsock WSAEACCES denial fixture, got %v", err)
	}
	if BindAddressInUse(err) || availabilityAddressUnavailable(err) {
		t.Fatal("access denial was treated as a harmless address/family result")
	}
	failure := availabilityError(err)
	if failure == nil || failure.Code != "permission_required" || failure.ExitCode != 3 {
		t.Fatalf("access denial not diagnosed: %v", failure)
	}
}
func TestWindowsBindErrorClassesRemainDistinct(t *testing.T) {
	for _, code := range []error{windows.WSAEACCES, windows.ERROR_ACCESS_DENIED} {
		err := &net.OpError{Op: "listen", Net: "tcp", Err: code}
		if !BindPermissionDenied(err) || BindAddressInUse(err) || BindFamilyUnavailable(err) {
			t.Fatalf("denial classification: %v", err)
		}
	}
	for _, code := range []error{windows.WSAEAFNOSUPPORT, windows.WSAEPROTONOSUPPORT} {
		if !BindFamilyUnavailable(code) || BindPermissionDenied(code) {
			t.Fatalf("family classification: %v", code)
		}
	}
}
