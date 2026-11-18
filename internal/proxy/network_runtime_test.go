package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func freeNetworkPort(t *testing.T) int {
	t.Helper()
	for range 32 {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		// Listener configuration reserves the number across all required
		// addresses. A free IPv4 loopback socket alone is insufficient.
		free, condition := ports.Available(port)
		if condition != nil {
			t.Fatal(condition)
		}
		if free {
			return port
		}
	}
	t.Fatal("cannot select a globally available TCP port for the network fixture")
	return 0
}
func liveNetworkManager(t *testing.T) Manager {
	return liveNetworkManagerWithControlListener(t, nil)
}

func liveNetworkManagerWithControlListener(t *testing.T, listen func() (net.Listener, error)) Manager {
	return liveNetworkFixture(t, listen, hosts.System{})
}

func liveNetworkFixture(t *testing.T, listen func() (net.Listener, error), system hosts.System) Manager {
	t.Helper()
	m := Manager{Data: t.TempDir(), ExposureProviders: map[string]expose.Provider{}, ControlListen: listen, hostSystem: system}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := false
	t.Cleanup(func() {
		cancel()
		if started {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("network daemon did not finish")
			}
		}
	})
	m.Spawn = func(_ string, id string) error {
		started = true
		go func() { defer close(done); _ = m.Serve(ctx, id) }()
		return nil
	}
	return m
}

func TestNetworkListenersHostsPending(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0600); err != nil {
		t.Fatal(err)
	}
	system := hosts.System{Path: path}
	m := liveNetworkFixture(t, nil, system)
	httpPort, tlsPort := freeNetworkPort(t), freeNetworkPort(t)
	for tlsPort == httpPort {
		tlsPort = freeNetworkPort(t)
	}
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", httpPort), fmt.Sprintf("127.0.0.1:%d", tlsPort), false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	hm := hosts.Manager{Data: m.Data, System: system, Run: func(ctx context.Context, _ string, input any, _ bool) *protocol.Error {
		return system.Apply(ctx, input.(hosts.Request))
	}}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	if _, e := m.AddGlobal(ctx, "ops", backend.Listener.Addr().(*net.TCPAddr).Port, "ops.test", "", "hosts", tasks.ID()); e != nil {
		t.Fatal(e)
	}
	p, projectRef := networkRoute(t, m, "app.test", "default", freeNetworkPort(t))
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.RouteUp(ctx, st.GlobalRoute("ops").Ref(), project.Context{}, nil, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.RouteUp(ctx, projectRef, p, nil, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	authority, e := (ca.Store{Data: m.Data}).Read()
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(authority.PEM())
	request := func() {
		t.Helper()
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "ops.test"},
		}}
		req, err := http.NewRequest("GET", fmt.Sprintf("https://127.0.0.1:%d/", tlsPort), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "ops.test"
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatal(resp.Status)
		}
	}
	request()
	hm.Run = func(context.Context, string, any, bool) *protocol.Error {
		return protocol.NewError("canceled", "fixture interrupted before applying", 130, nil)
	}
	if _, e := m.RouteDown(ctx, projectRef, hm, tasks.ID()); e == nil {
		t.Fatal("expected interrupted hosts cleanup")
	}
	request()
}

// Network control returns proxy_pending while its bounded lease probe or a
// control response is uncertain.
// Keep the operation identity frozen while establishing a definitive fixture
// state. Other errors (including bind refusal) remain visible to the caller.
func settledNetworkApply(t *testing.T, ctx context.Context, m Manager, action, name string, all bool, id string) (any, *protocol.Error) {
	t.Helper()
	ready, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	for {
		result, failure := m.NetworkApply(ready, action, name, all, id)
		if failure == nil || failure.Code != "proxy_pending" || ready.Err() != nil {
			if failure != nil {
				t.Logf("network fixture result: code=%s details=%v", failure.Code, failure.Details)
			}
			return result, failure
		}
		t.Logf("resume same network operation after pending: %v", failure.Details)
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ready.Done():
			timer.Stop()
			return result, failure
		case <-timer.C:
		}
	}
}

func networkRoute(t *testing.T, m Manager, host, listener string, backend int) (project.Context, network.RouteRef) {
	t.Helper()
	dir := t.TempDir()
	content := fmt.Sprintf("profile='app'\n[ports.web]\nport=%d\n[proxies.app]\nhost='%s'\nport='web'\nlistener='%s'\n", backend, host, listener)
	if e := os.WriteFile(filepath.Join(dir, "devtools.toml"), []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(dir, "")
	if e != nil {
		t.Fatal(e)
	}
	instance := register(t, m.portStore(), p.Profile, p.Root, nil, backend)
	return p, network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: "app"}
}
func TestNetworkListenersTLSRedirectAndIndependentLifecycle(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Header.Get("X-Forwarded-Proto")+" "+r.URL.RequestURI())
	}))
	defer backend.Close()
	port := backend.Listener.Addr().(*net.TCPAddr).Port
	otherBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "other")
	}))
	defer otherBackend.Close()
	otherBackendPort := otherBackend.Listener.Addr().(*net.TCPAddr).Port
	httpPort, tlsPort, otherPort := freeNetworkPort(t), freeNetworkPort(t), freeNetworkPort(t)
	for _, v := range []struct{ name, http, https string }{{"default", fmt.Sprintf("127.0.0.1:%d", httpPort), fmt.Sprintf("127.0.0.1:%d", tlsPort)}, {"other", fmt.Sprintf("127.0.0.1:%d", otherPort), ""}} {
		if _, e := m.Configure(ctx, v.name, v.http, v.https, false, tasks.ID()); e != nil {
			t.Fatal(e)
		}
	}
	networkRoute(t, m, "app.localhost", "default", port)
	otherProject, otherRef := networkRoute(t, m, "other.localhost", "other", otherBackendPort)
	id := tasks.ID()
	if _, e := settledNetworkApply(t, ctx, m, "start", "", true, id); e != nil {
		t.Fatal(e)
	}
	if names, issue := m.CompletionListeners(ctx, true, "oth"); issue != nil || len(names) != 1 || names[0] != "other" {
		t.Fatal("running listener missing from stop completion", names, issue)
	}
	if _, e := m.RouteUp(ctx, otherRef, otherProject, nil, hosts.Manager{Data: m.Data, Isolated: true}, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/path?x=1", httpPort), nil)
	req.Host = "app.localhost"
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 307 || res.Header.Get("Location") != fmt.Sprintf("https://app.localhost:%d/path?x=1", tlsPort) {
		t.Fatal(res.Status, res.Header)
	}
	a, e2 := (ca.Store{Data: m.Data}).Read()
	if e2 != nil {
		t.Fatal(e2)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(a.PEM())
	transport := &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "app.localhost"}, Proxy: nil}
	defer transport.CloseIdleConnections()
	// The cold TLS resolver and HTTP handler both inspect private storage;
	// a legitimate sharing retry alone can take two seconds. This functional
	// HTTP/2/lifecycle check has no one-second performance contract.
	secure := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	req, _ = http.NewRequest("GET", fmt.Sprintf("https://127.0.0.1:%d/path?x=1", tlsPort), nil)
	req.Host = "app.localhost"
	req.Header.Set("X-Forwarded-Proto", "spoof")
	started := time.Now()
	var traceMu sync.Mutex
	var phases []string
	phase := func(name string) {
		traceMu.Lock()
		defer traceMu.Unlock()
		phases = append(phases, fmt.Sprintf("%s=%s", name, time.Since(started).Round(time.Millisecond)))
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		TLSHandshakeStart:    func() { phase("tls_start") },
		TLSHandshakeDone:     func(_ tls.ConnectionState, err error) { phase(fmt.Sprintf("tls_done_ok_%t", err == nil)) },
		WroteRequest:         func(info httptrace.WroteRequestInfo) { phase(fmt.Sprintf("request_written_ok_%t", info.Err == nil)) },
		GotFirstResponseByte: func() { phase("first_response_byte") },
	}))
	res, e = secure.Do(req)
	traceMu.Lock()
	t.Logf("single cold TLS request phases: %v", phases)
	traceMu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.ProtoMajor != 2 {
		t.Fatalf("TLS listener did not negotiate HTTP/2: %s", res.Proto)
	}
	if string(body) != "https /path?x=1" {
		t.Fatal(string(body))
	}
	otherBefore, e2 := m.NetworkStatus(ctx, "other")
	if e2 != nil {
		t.Fatal(e2)
	}
	if _, e := settledNetworkApply(t, ctx, m, "stop", "default", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if names, issue := m.CompletionListeners(ctx, true, "def"); issue != nil || len(names) != 0 {
		t.Fatal("stopped listener suggested for stop", names, issue)
	}
	if names, issue := m.CompletionListeners(ctx, false, "def"); issue != nil || len(names) != 1 || names[0] != "default" {
		t.Fatal("stopped listener missing from inspection completion", names, issue)
	}
	if _, e := secure.Do(req); e == nil {
		t.Fatal("stopped TLS socket remained reachable")
	}
	otherAfter, e2 := m.NetworkStatus(ctx, "other")
	if e2 != nil || otherAfter["running"] != true || otherBefore["generation"] != otherAfter["generation"] {
		t.Fatal(otherAfter, e2)
	}
	otherRequest, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", otherPort), nil)
	otherRequest.Host = "other.localhost"
	otherResponse, err := client.Do(otherRequest)
	if err != nil {
		t.Fatal(err)
	}
	otherBody, _ := io.ReadAll(otherResponse.Body)
	otherResponse.Body.Close()
	if otherResponse.StatusCode != http.StatusOK || string(otherBody) != "other" {
		t.Fatal(otherResponse.StatusCode, string(otherBody))
	}
	// A completed receipt reports history without starting the stopped listener.
	if _, e := settledNetworkApply(t, ctx, m, "start", "", true, id); e != nil {
		t.Fatal(e)
	}
	status, e2 := m.NetworkStatus(ctx, "default")
	if e2 != nil || status["running"] != false {
		t.Fatal(status, e2)
	}
	if _, e := m.Configure(ctx, "other", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e == nil || e.Code != "listener_running" {
		t.Fatal(e)
	}
	logs, e2 := m.NetworkLogs("default", false)
	if e2 != nil || len(logs) != 2 || logs[0].Listener != "default" || logs[1].State != "stopped" {
		t.Fatal(logs, e2)
	}
	if _, e := settledNetworkApply(t, ctx, m, "restart", "default", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := settledNetworkApply(t, ctx, m, "stop", "", true, tasks.ID()); e != nil {
		t.Fatal(e)
	}
}
func TestNetworkSecondSocketFailureRollsBackFirst(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	httpPort, tlsPort := freeNetworkPort(t), freeNetworkPort(t)
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", httpPort), fmt.Sprintf("127.0.0.1:%d", tlsPort), false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	occupied, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", tlsPort))
	if e != nil {
		t.Fatal(e)
	}
	defer occupied.Close()
	if _, e := settledNetworkApply(t, ctx, m, "start", "default", false, tasks.ID()); e == nil || e.Code != "port_in_use" {
		t.Fatal(e)
	}
	probe, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", httpPort))
	if e != nil {
		t.Fatal("HTTP socket leaked:", e)
	}
	probe.Close()
}
func TestTLSRejectsUnknownSNI(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	port := freeNetworkPort(t)
	if _, e := m.Configure(ctx, "tls", "", fmt.Sprintf("127.0.0.1:%d", port), false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, failure := settledNetworkApply(t, ctx, m, "start", "tls", false, tasks.ID()); failure != nil {
		t.Fatalf("TLS-only start: code=%s details=%v", failure.Code, failure.Details)
	}
	conn, e := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{InsecureSkipVerify: true, ServerName: "unknown.localhost"})
	if conn != nil {
		conn.Close()
	}
	if e == nil {
		t.Fatal(e)
	}
}
