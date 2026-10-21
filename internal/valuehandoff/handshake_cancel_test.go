package valuehandoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestCanceledRunnerHelloClosesBeforeInputDeadline(t *testing.T) {
	testCanceledRunnerHandshake(t, false)
}
func TestCanceledRegistrationProbeCreatesNoReceipt(t *testing.T) {
	testCanceledRunnerHandshake(t, true)
}
func testCanceledRunnerHandshake(t *testing.T, probe bool) {
	t.Helper()
	s, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runner, instance := requestID(900), requestID(901)
	listener, err := vault.ListenPeer(ctx, s.Root, runner, instance)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address, err := Address(s.Root, runner, instance)
	if err != nil {
		t.Fatal(err)
	}
	accepted, release := make(chan struct{}), make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		c, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		defer c.Close()
		if probe {
			h := helloFrame{1, "hello", address.RootID, address.VaultID, runner, instance, "0123456789abcdef0123456789abcdef"}
			if writeFrame(c, h, maxControlFrame) != nil {
				return
			}
			var p probeFrame
			if readControl(c, &p) != nil {
				return
			}
		}
		close(accepted)
		<-release
	}()
	defer func() {
		close(release)
		listener.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("owned handshake peer did not close")
		}
	}()
	connectCtx, cancelConnect := context.WithTimeout(ctx, 5*time.Second)
	defer cancelConnect()
	done := make(chan error, 1)
	go func() {
		if probe {
			_, e := s.Register(connectCtx, RunnerInput{runner, instance, 1, 1}, requestID(902))
			if e == nil {
				done <- nil
				return
			}
			if e.Code != "canceled" {
				t.Errorf("registration lost cancellation classification: %s", e.Code)
			}
			done <- e
			return
		}
		c, _, _, err := connectRunner(connectCtx, s.Root, address)
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	select {
	case <-accepted:
	case <-serverDone:
		t.Fatal("owned handshake peer closed before the cancellation boundary")
	case <-ctx.Done():
		t.Fatal("owned peer did not accept")
	}
	cancelConnect()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled hello succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled hello waited for the original input deadline")
	}
	if probe {
		if _, err := os.Stat(filepath.Join(s.Root, s.path("value-runner-requests", requestID(902)))); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("canceled registration consumed request UUID", err)
		}
	}
}
