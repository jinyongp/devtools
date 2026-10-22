package proxy

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestControlListenerAvoidsRegisteredPorts(t *testing.T) {
	for _, kind := range []string{"reservation", "assignment"} {
		t.Run(kind, func(t *testing.T) {
			m := testManager(t)
			claimed := testPort()
			if kind == "reservation" {
				if _, _, e := m.portStore().Reserve(context.Background(), "proxy", claimed); e != nil {
					t.Fatal(e)
				}
			} else {
				register(t, m.portStore(), "app", t.TempDir(), nil, claimed)
			}
			rejected, accepted := newIdleListener(claimed), newIdleListener(testPort())
			t.Cleanup(func() { _ = rejected.Close(); _ = accepted.Close() })
			calls := 0
			m.ControlListen = func() (net.Listener, error) {
				calls++
				if calls == 1 {
					return rejected, nil
				}
				return accepted, nil
			}
			socket, err := m.listenControl(context.Background())
			if err != nil || socket != accepted || !rejected.isClosed() || accepted.isClosed() {
				t.Fatalf("control allocation: socket=%v error=%v rejected_closed=%t accepted_closed=%t", socket, err, rejected.isClosed(), accepted.isClosed())
			}
		})
	}
}

func TestControlListenerBoundsCollisionsAndClosesRejectedSockets(t *testing.T) {
	m := testManager(t)
	claimed := testPort()
	if _, _, e := m.portStore().Reserve(context.Background(), "proxy", claimed); e != nil {
		t.Fatal(e)
	}
	var sockets []*idleListener
	unbounded := errors.New("control allocation did not bound its retries")
	m.ControlListen = func() (net.Listener, error) {
		if len(sockets) >= 32 {
			return nil, unbounded
		}
		socket := newIdleListener(claimed)
		sockets = append(sockets, socket)
		return socket, nil
	}
	socket, err := m.listenControl(context.Background())
	if socket != nil || err == nil || errors.Is(err, unbounded) || len(sockets) == 0 {
		t.Fatalf("collision exhaustion: socket=%v error=%v attempts=%d", socket, err, len(sockets))
	}
	for _, rejected := range sockets {
		if !rejected.isClosed() {
			t.Error("rejected control socket remained open")
			_ = rejected.Close()
		}
	}
}

func TestControlListenerPropagatesListenFailureAndCancellation(t *testing.T) {
	m := testManager(t)
	failure := errors.New("control bind denied")
	calls := 0
	m.ControlListen = func() (net.Listener, error) { calls++; return nil, failure }
	if socket, err := m.listenControl(context.Background()); socket != nil || !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("bind failure: socket=%v error=%v calls=%d", socket, err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if socket, err := m.listenControl(ctx); socket != nil || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled allocation: socket=%v error=%v calls=%d", socket, err, calls)
	}
}

func TestControlListenerClosesSocketWhenRegistryCannotBeRead(t *testing.T) {
	m := testManager(t)
	m.Data = filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(m.Data, nil, 0600); err != nil {
		t.Fatal(err)
	}
	candidate := newIdleListener(testPort())
	t.Cleanup(func() { _ = candidate.Close() })
	m.ControlListen = func() (net.Listener, error) { return candidate, nil }
	if socket, err := m.listenControl(context.Background()); socket != nil || err == nil || !candidate.isClosed() {
		t.Fatalf("registry failure: socket=%v error=%v closed=%t", socket, err, candidate.isClosed())
	}
}
