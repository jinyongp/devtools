package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestListenerStopAndRestartCloseOnlyOwnedBackendPool(t *testing.T) {
	first, second := newExposureBackend(t, nil), newExposureBackend(t, nil)
	m := liveNetworkManager(t)
	ctx := context.Background()
	firstPort, secondPort := freeNetworkPort(t), freeNetworkPort(t)
	for _, listener := range []struct {
		name string
		port int
	}{{"default", firstPort}, {"other", secondPort}} {
		if _, e := m.Configure(ctx, listener.name, fmt.Sprintf("127.0.0.1:%d", listener.port), "", false, tasks.ID()); e != nil {
			t.Fatal(e)
		}
	}
	networkRoute(t, m, "first.localhost", "default", first.server.Listener.Addr().(*net.TCPAddr).Port)
	p, ref := networkRoute(t, m, "second.localhost", "other", second.server.Listener.Addr().(*net.TCPAddr).Port)
	if _, e := m.NetworkApply(ctx, "start", "", true, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.RouteUp(ctx, ref, p, nil, hosts.Manager{Data: m.Data, Isolated: true}, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	call := func(port int, host string) {
		t.Helper()
		request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
		request.Host = host
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		_, e = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if e != nil || response.StatusCode != 200 {
			t.Fatal(response.Status, e)
		}
	}
	call(firstPort, "first.localhost")
	call(secondPort, "second.localhost")
	first.waitOpen(t, 1)
	second.waitOpen(t, 1)
	if _, e := m.NetworkApply(ctx, "restart", "default", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	first.waitOpen(t, 0)
	second.waitOpen(t, 1)
	call(firstPort, "first.localhost")
	call(secondPort, "second.localhost")
	if first.accepted.Load() != 2 || second.accepted.Load() != 1 {
		t.Fatal("restart disturbed another listener's pool")
	}
	if _, e := m.NetworkApply(ctx, "stop", "default", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	first.waitOpen(t, 0)
	second.waitOpen(t, 1)
	if _, e := m.stop(ctx); e != nil {
		t.Fatal(e)
	}
	second.waitOpen(t, 0)
}

func TestLegacyProxyStopClosesBackendPool(t *testing.T) {
	backend := newExposureBackend(t, nil)
	m := testManager(t)
	directory := t.TempDir()
	writeConfig(t, directory, "app", "legacy.localhost")
	register(t, m.portStore(), "app", directory, nil, backend.server.Listener.Addr().(*net.TCPAddr).Port)
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	m.Listen = func(int) ([]net.Listener, error) { return []net.Listener{listener}, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := false
	t.Cleanup(func() {
		cancel()
		if started {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("legacy daemon did not stop")
			}
		}
	})
	m.Spawn = func(_ string, id string) error { started = true; go func() { done <- m.Serve(ctx, id) }(); return nil }
	if _, e := m.Apply(ctx, Request{Action: "start", Port: &port, RequestID: tasks.ID()}); e != nil {
		t.Fatal(e)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	request.Host = "legacy.localhost"
	response, e := client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if e != nil || response.StatusCode != 200 {
		t.Fatal(response.Status, e)
	}
	backend.waitOpen(t, 1)
	if _, e := m.Apply(ctx, Request{Action: "stop", RequestID: tasks.ID()}); e != nil {
		t.Fatal(e)
	}
	backend.waitOpen(t, 0)
}
