package ports

import (
	"net"
	"testing"
)

func TestAvailableDetectsOccupiedListeners(t *testing.T) {
	for _, target := range []probeAddress{
		{"tcp4", "0.0.0.0"},
		{"tcp4", "127.0.0.1"},
		{"tcp6", "::"},
		{"tcp6", "::1"},
	} {
		t.Run(target.network+"/"+target.host, func(t *testing.T) {
			listener, err := net.Listen(target.network, net.JoinHostPort(target.host, "0"))
			if err != nil {
				t.Skipf("address unavailable: %v", err)
			}
			defer listener.Close()
			port := listener.Addr().(*net.TCPAddr).Port
			if free, err := Available(port); err != nil || free {
				t.Fatalf("occupied port reported free=%v, error=%v", free, err)
			}
		})
	}
}
