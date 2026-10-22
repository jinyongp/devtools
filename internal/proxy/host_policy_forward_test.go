package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestHostPolicyHTTPAndWebSocket(t *testing.T) {
	for _, policy := range []network.HostPolicy{"", network.HostRewrite, network.HostPreserve} {
		for _, secure := range []bool{false, true} {
			for _, upgrade := range []bool{false, true} {
				for _, authority := range []string{"app.example.com", "APP.EXAMPLE.COM:8443", "app.example.com:443"} {
					t.Run(fmt.Sprintf("%s/tls=%t/ws=%t/%s", policy, secure, upgrade, authority), func(t *testing.T) {
						observed := make(chan *http.Request, 1)
						backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							observed <- r.Clone(r.Context())
							if !upgrade {
								fmt.Fprint(w, r.URL.RequestURI())
								return
							}
							conn, buffer, err := w.(http.Hijacker).Hijack()
							if err != nil {
								t.Error(err)
								return
							}
							defer conn.Close()
							buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
							buffer.Flush()
							var payload [4]byte
							if _, err := io.ReadFull(buffer, payload[:]); err != nil {
								t.Error(err)
								return
							}
							conn.Write(payload[:])
						}))
						defer backend.Close()
						handler := NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
							item := readyRoute("app.example.com", targetPort(t, backend))
							item.HostPolicy = policy
							return []Item{item}, nil
						}))
						front := httptest.NewUnstartedServer(handler)
						if secure {
							front.StartTLS()
						} else {
							front.Start()
						}
						defer front.Close()
						var conn net.Conn
						var err error
						if secure {
							config := front.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
							config.ServerName = "app.example.com"
							conn, err = tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", front.Listener.Addr().String(), config)
						} else {
							conn, err = net.DialTimeout("tcp", front.Listener.Addr().String(), time.Second)
						}
						if err != nil {
							t.Fatal(err)
						}
						defer conn.Close()
						conn.SetDeadline(time.Now().Add(3 * time.Second))
						origin := "https://" + strings.ToLower(authority)
						req, _ := http.NewRequest("GET", front.URL+"/socket?q=1", nil)
						req.Host = authority
						req.Header.Set("Origin", origin)
						req.Header.Set("X-Forwarded-Host", "spoof.example.com")
						req.Header.Set("X-Forwarded-Proto", "spoof")
						req.Header.Set("Forwarded", "host=spoof.example.com")
						if upgrade {
							req.Header.Set("Connection", "Upgrade")
							req.Header.Set("Upgrade", "websocket")
						}
						if err := req.Write(conn); err != nil {
							t.Fatal(err)
						}
						reader := bufio.NewReader(conn)
						response, err := http.ReadResponse(reader, req)
						if err != nil {
							t.Fatal(err)
						}
						if upgrade {
							if response.StatusCode != 101 {
								t.Fatal(response.Status)
							}
							// A small unmasked server text frame verifies bidirectional bytes,
							// in addition to the upgrade response.
							frame := []byte{0x81, 2, 'o', 'k'}
							conn.Write(frame)
							got := make([]byte, len(frame))
							if _, err := io.ReadFull(reader, got); err != nil || string(got) != string(frame) {
								t.Fatalf("WS bytes %q: %v", got, err)
							}
						} else {
							body, _ := io.ReadAll(response.Body)
							response.Body.Close()
							if response.StatusCode != 200 || string(body) != "/socket?q=1" {
								t.Fatal(response.Status, string(body))
							}
						}
						got := <-observed
						wantHost := backend.Listener.Addr().String()
						if policy == network.HostPreserve {
							wantHost = authority
						}
						wantProto := "http"
						if secure {
							wantProto = "https"
						}
						if got.Host != wantHost || got.Header.Get("Origin") != origin || got.Header.Get("X-Forwarded-Host") != authority || got.Header.Get("X-Forwarded-Proto") != wantProto || got.Header.Get("Forwarded") != "" {
							t.Fatalf("Host=%s headers=%v", got.Host, got.Header)
						}
					})
				}
			}
		}
	}
}

func TestHostPolicyPreservesOriginGuard(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.Host != r.Host {
			http.Error(w, "origin rejected", 403)
			return
		}
		w.WriteHeader(204)
	}))
	defer backend.Close()
	h := NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
		item := readyRoute("app.localhost", targetPort(t, backend))
		item.HostPolicy = network.HostPreserve
		return []Item{item}, nil
	}))
	for _, origin := range []string{"https://app.localhost:8443", "https://other.localhost:8443", "https://app.localhost:9443", "null", ""} {
		req := httptest.NewRequest("GET", "http://app.localhost:8443/", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Forwarded-Host", "other.localhost:8443")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		want := 403
		if origin == "https://app.localhost:8443" {
			want = 204
		}
		if w.Code != want {
			t.Fatalf("origin %q: %d", origin, w.Code)
		}
	}
}

func TestHostPolicyInvalidNeverDials(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer backend.Close()
	h := NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
		item := readyRoute("app.localhost", targetPort(t, backend))
		item.HostPolicy = "unsafe"
		return []Item{item}, nil
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://app.localhost/", nil))
	if w.Code != 503 || calls.Load() != 0 {
		t.Fatal(w.Code, calls.Load())
	}
}
