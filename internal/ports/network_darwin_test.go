//go:build darwin

package ports

import (
	"errors"
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"
)

func TestDarwinLowPortAvailabilityDoesNotRequireSpecificBindPrivilege(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged process")
	}
	for _, port := range []int{80, 443} {
		// Either occupied or available is valid; lack of permission to bind an
		// unrelated specific address must not become a storage error.
		if _, err := Available(port); err != nil {
			t.Fatalf("low port %d: %v", port, err)
		}
	}
}

func TestDarwinExclusiveWildcardProbeDetectsSpecificListener(t *testing.T) {
	for _, target := range []probeAddress{{"tcp4", "127.0.0.1"}, {"tcp6", "::1"}} {
		t.Run(target.network, func(t *testing.T) {
			listener, err := net.Listen(target.network, net.JoinHostPort(target.host, "0"))
			if err != nil {
				t.Skipf("address unavailable: %v", err)
			}
			defer listener.Close()
			port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
			wildcard := "0.0.0.0"
			if target.network == "tcp6" {
				wildcard = "::"
			}
			probe, err := listenExclusiveAvailability(probeAddress{target.network, wildcard}, port)
			if probe != nil {
				probe.Close()
			}
			if !errors.Is(err, syscall.EADDRINUSE) {
				t.Fatalf("specific listener missed by exclusive wildcard probe: %v", err)
			}
		})
	}
}
