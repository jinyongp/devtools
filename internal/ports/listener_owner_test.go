package ports

import (
	"context"
	"net"
	"os"
	"runtime"
	"testing"
)

func TestListenerObservationFindsTheRealListenerWithoutExecutionRegistration(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("native listener observation is unsupported")
	}
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			host := "127.0.0.1:0"
			if network == "tcp6" {
				host = "[::1]:0"
			}
			listener, err := net.Listen(network, host)
			if err != nil {
				t.Skipf("%s is unavailable: %v", network, err)
			}
			defer listener.Close()
			observed := ObserveListener(context.Background(), listener.Addr().(*net.TCPAddr).Port)
			for _, process := range observed.Processes {
				if process.PID == os.Getpid() && process.Program != "" {
					return
				}
			}
			t.Fatalf("the actual listener was not identified: %#v", observed)
		})
	}
}
