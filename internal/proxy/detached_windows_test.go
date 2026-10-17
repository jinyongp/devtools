package proxy

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "__proxy-serve" {
		if (Manager{Data: os.Args[2]}).Serve(context.Background(), os.Args[3]) != nil {
			os.Exit(90)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestWindowsDetachedHTTPProxyLifecycleAndAuthorization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	data := filepath.Join(t.TempDir(), "data")
	manager := Manager{Data: data}
	listener, err := ports.ListenTCP(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	request := Request{Action: "start", Port: &port, RequestID: tasks.ID()}
	first, failure := manager.Apply(ctx, request)
	if failure != nil || !first.Running {
		t.Fatalf("start: %+v %v", first, failure)
	}
	t.Cleanup(func() { manager.Apply(context.Background(), Request{Action: "stop", RequestID: tasks.ID()}) })
	replay, failure := manager.Apply(ctx, request)
	if failure != nil || !replay.Replayed || !replay.Running {
		t.Fatalf("replay: %+v %v", replay, failure)
	}
	status, failure := manager.Status(ctx)
	if failure != nil || !status.Running {
		t.Fatalf("status: %+v %v", status, failure)
	}
	// A real HTTP request traverses the daemon listener and fails closed on an unknown route.
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(port)+"/", nil)
	req.Host = "missing.localhost"
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("unknown route=%d", response.StatusCode)
	}
	var control proxyControl
	if err := tasks.ReadPrivate(manager.path("control.json"), &control); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"token", "origin", "host", "method"} {
		req, _ := http.NewRequestWithContext(ctx, "POST", control.Address+"/stop", nil)
		req.Header.Set("Authorization", "Bearer "+control.Token)
		switch kind {
		case "token":
			req.Header.Set("Authorization", "Bearer wrong")
		case "origin":
			req.Header.Set("Origin", "http://outside.invalid")
		case "host":
			req.Host = "outside.invalid"
		case "method":
			req.Method = "GET"
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("%s auth=%d", kind, resp.StatusCode)
		}
	}
	stopped, failure := manager.Apply(ctx, Request{Action: "stop", RequestID: tasks.ID()})
	if failure != nil || stopped.Running {
		t.Fatalf("stop: %+v %v", stopped, failure)
	}
	status, failure = manager.Status(ctx)
	if failure != nil || status.Running {
		t.Fatalf("stopped status: %+v %v", status, failure)
	}
	// Released sockets can be exclusively rebound after stop.
	sockets, err := ListenLoopback(port)
	if err != nil {
		t.Fatal(err)
	}
	for _, socket := range sockets {
		socket.Close()
	}
}
