package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
)

type routeListFunc func(context.Context, string) ([]Item, *protocol.Error)

func (function routeListFunc) List(ctx context.Context, profile string) ([]Item, *protocol.Error) {
	return function(ctx, profile)
}

func readyRoute(host string, port int) Item {
	name, service := "app", "web"
	return Item{Kind: "route", Host: &host, Proxy: &name, Service: &service, TargetPort: &port, Status: StatusReady}
}

func targetPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	_, rawPort, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscan(rawPort, &port); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestHandlerForwardsRequestAndHeaders(t *testing.T) {
	var backend *httptest.Server
	backend = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != "request-body" {
			t.Errorf("upstream body = %q %v", body, err)
		}
		if request.Host != backend.Listener.Addr().String() {
			t.Errorf("upstream host = %q", request.Host)
		}
		if request.Header.Get("X-Forwarded-Host") != "APP.LOCALHOST:20200" || request.Header.Get("X-Forwarded-Proto") != "http" {
			t.Errorf("forwarded headers = %q %q", request.Header.Get("X-Forwarded-Host"), request.Header.Get("X-Forwarded-Proto"))
		}
		response.Header().Set("X-Backend", "yes")
		_, _ = io.WriteString(response, "proxied:"+request.URL.RequestURI())
	}))
	defer backend.Close()
	port := targetPort(t, backend)
	front := httptest.NewServer(NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
		return []Item{readyRoute("app.localhost", port)}, nil
	})))
	defer front.Close()

	request, _ := http.NewRequest(http.MethodPost, front.URL+"/hello?q=1", strings.NewReader("request-body"))
	request.Host = "APP.LOCALHOST:20200"
	response, err := front.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Backend") != "yes" || string(body) != "proxied:/hello?q=1" {
		t.Fatalf("response = %d %q %q", response.StatusCode, response.Header.Get("X-Backend"), body)
	}
}

func TestHandlerRouteAndBackendFailures(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedPort := targetPort(t, closed)
	closed.Close()
	host := "conflict.localhost"
	tests := []struct {
		name   string
		host   string
		items  []Item
		status int
	}{
		{name: "unknown", host: "unknown.localhost", status: http.StatusNotFound},
		{name: "invalid host", host: "bad:host:value", status: http.StatusNotFound},
		{name: "conflict", host: host, items: []Item{{Kind: "route", Host: &host, Status: StatusHostConflict}}, status: http.StatusServiceUnavailable},
		{name: "backend", host: "down.localhost", items: []Item{readyRoute("down.localhost", closedPort)}, status: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://listener/", nil)
			request.Host = test.host
			response := httptest.NewRecorder()
			NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) { return test.items, nil })).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if strings.Contains(response.Body.String(), "/") || strings.Contains(response.Body.String(), "dial") {
				t.Fatalf("internal detail leaked: %q", response.Body.String())
			}
		})
	}
}

func TestHandlerStreamsResponses(t *testing.T) {
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "first\n")
		response.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(response, "second\n")
	}))
	defer backend.Close()
	front := httptest.NewServer(NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
		return []Item{readyRoute("stream.localhost", targetPort(t, backend))}, nil
	})))
	defer front.Close()
	request, _ := http.NewRequest(http.MethodGet, front.URL, nil)
	request.Host = "stream.localhost"
	response, err := front.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "first\n" {
		t.Fatalf("first flush = %q %v", line, err)
	}
	close(release)
	remainder, _ := io.ReadAll(reader)
	_ = response.Body.Close()
	if string(remainder) != "second\n" {
		t.Fatalf("remainder = %q", remainder)
	}
}

func TestHandlerForwardsWebSocketUpgrade(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
			http.Error(response, "upgrade required", http.StatusBadRequest)
			return
		}
		connection, buffer, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\nhello")
		_ = buffer.Flush()
	}))
	defer backend.Close()
	front := httptest.NewServer(NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
		return []Item{readyRoute("socket.localhost", targetPort(t, backend))}, nil
	})))
	defer front.Close()
	connection, err := net.DialTimeout("tcp", front.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = fmt.Fprintf(connection, "GET /socket HTTP/1.1\r\nHost: socket.localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestHandlerIgnoresOutboundProxyEnvironment(t *testing.T) {
	var sentinelConnections atomic.Int32
	sentinel := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sentinelConnections.Add(1) }))
	defer sentinel.Close()
	t.Setenv("HTTP_PROXY", sentinel.URL)
	t.Setenv("HTTPS_PROXY", sentinel.URL)
	t.Setenv("ALL_PROXY", sentinel.URL)
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(response, "direct") }))
	defer backend.Close()
	front := httptest.NewServer(NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
		return []Item{readyRoute("direct.localhost", targetPort(t, backend))}, nil
	})))
	defer front.Close()
	request, _ := http.NewRequest(http.MethodGet, front.URL, nil)
	request.Host = "direct.localhost"
	response, err := front.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "direct" || sentinelConnections.Load() != 0 {
		t.Fatalf("body = %q, sentinel = %d", body, sentinelConnections.Load())
	}
}

func TestHandlerUsesCurrentAssignmentWithoutRestart(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "first")
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "second")
	}))
	defer second.Close()

	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "app", "app.localhost")
	alias := "main"
	instance := register(t, store, "app", directory, &alias, targetPort(t, first))
	front := httptest.NewServer(NewHandler(Resolver{Ports: store}))
	defer front.Close()
	request := func() string {
		req, _ := http.NewRequest(http.MethodGet, front.URL, nil)
		req.Host = "app.localhost"
		response, err := front.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return string(body)
	}
	if body := request(); body != "first" {
		t.Fatalf("first backend = %q", body)
	}
	if err := store.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		state.Get(instance.ID, "web").Port = targetPort(t, second)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if body := request(); body != "second" {
		t.Fatalf("second backend = %q", body)
	}
}
