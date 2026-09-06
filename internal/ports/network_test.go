package ports

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestAvailableDetectsLocalListeners(t *testing.T) {
	targets, err := localProbeAddresses()
	if err != nil {
		t.Fatal(err)
	}
	targets = append(targets, probeAddress{"tcp4", "0.0.0.0"}, probeAddress{"tcp6", "::"})
	for _, target := range targets {
		t.Run(target.host, func(t *testing.T) {
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

func TestAvailableAfterServerClosesConnection(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server.Close()
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("server did not close connection: %v", err)
	}
	client.Close()
	listener.Close()
	if free, err := Available(port); err != nil || !free {
		t.Fatalf("closed server reported free=%v, error=%v", free, err)
	}
}
