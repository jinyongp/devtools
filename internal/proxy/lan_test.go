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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestLANAliasTLSAuthStopAndLocalRoutePreservation(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	var backendRequests atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendRequests.Add(1)
		fmt.Fprint(w, r.Header.Get("X-Forwarded-Proto")+"|"+r.Header.Get("X-Forwarded-Host"))
	}))
	defer backend.Close()
	port := backend.Listener.Addr().(*net.TCPAddr).Port
	httpPort, tlsPort := freeNetworkPort(t), freeNetworkPort(t)
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("0.0.0.0:%d", httpPort), fmt.Sprintf("0.0.0.0:%d", tlsPort), false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.AddGlobal(ctx, "app", port, "app.localhost", "", "auto", tasks.ID()); e != nil {
		t.Fatal(e)
	}
	st, _ := m.portStore().Read()
	ref := st.GlobalRoute("app").Ref()
	result, e := m.ExposeStart(ctx, ExposureRequest{Route: ref, Provider: "lan", Auth: "reader:canary", RequestID: tasks.ID()}, project.Context{}, nil, hosts.Manager{Data: m.Data, Isolated: true})
	if e != nil {
		t.Fatal(e)
	}
	item := result.(map[string]any)["item"].(expose.Public)
	if item.Host != "app.local" || !strings.Contains(strings.Join(item.ClientSetup, " "), "does not advertise mDNS") {
		t.Fatal(item)
	}
	plainTransport := &http.Transport{Proxy: nil}
	defer plainTransport.CloseIdleConnections()
	plain := &http.Client{Transport: plainTransport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, host := range []string{item.Host, fmt.Sprintf("%s:%d", item.Host, httpPort), fmt.Sprintf("%s:%d", item.Host, tlsPort)} {
		for _, authenticated := range []bool{false, true} {
			request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/a%%2Fb?q=one%%20two", httpPort), nil)
			request.Host = host
			request.Header.Set("X-Forwarded-Proto", "https")
			if authenticated {
				request.SetBasicAuth("reader", "canary")
			}
			response, err := plain.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusTemporaryRedirect || response.Header.Get("Location") != item.URL+"/a%2Fb?q=one%20two" || response.Header.Get("WWW-Authenticate") != "" || backendRequests.Load() != 0 {
				t.Fatal("plaintext LAN request reached authentication/backend", host, response.Status, response.Header)
			}
		}
	}
	authority, e := (ca.Store{Data: m.Data}).Read()
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(authority.PEM())
	call := func(host, auth string) (int, string, error) {
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: host}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: time.Second}
		r, _ := http.NewRequest("GET", fmt.Sprintf("https://127.0.0.1:%d/", tlsPort), nil)
		r.Host = fmt.Sprintf("%s:%d", host, tlsPort)
		if auth != "" {
			r.SetBasicAuth("reader", auth)
		}
		res, e := client.Do(r)
		if e != nil {
			return 0, "", e
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body), nil
	}
	if code, _, e := call("app.local", ""); e != nil || code != 401 {
		t.Fatal(code, e)
	}
	if code, body, e := call("app.local", "canary"); e != nil || code != 200 || body != fmt.Sprintf("https|app.local:%d", tlsPort) {
		t.Fatal(code, body, e)
	}
	if _, e := m.ExposeStop(ctx, item.ID, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, _, e := call("app.local", "canary"); e == nil {
		t.Fatal("stopped LAN alias still accepts TLS")
	}
	if code, _, e := call("app.localhost", ""); e != nil || code != 200 {
		t.Fatal("local route affected by LAN stop", code, e)
	}
}

func TestLANRefusesLoopbackOnlyAndInvalidAliases(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.AddGlobal(ctx, "app", freeNetworkPort(t), "app.localhost", "", "auto", tasks.ID()); e != nil {
		t.Fatal(e)
	}
	st, _ := m.portStore().Read()
	ref := st.GlobalRoute("app").Ref()
	for _, alias := range []string{"", "app.example"} {
		_, e := m.ExposeStart(ctx, ExposureRequest{Route: ref, Provider: "lan", Alias: alias, NoAuth: true, RequestID: tasks.ID()}, project.Context{}, nil, hosts.Manager{Data: m.Data, Isolated: true})
		if e == nil || e.Code != "lan_listener_required" && e.Code != "invalid_lan_alias" {
			t.Fatal(e)
		}
	}
	st, _ = m.portStore().Read()
	if len(st.Network.Pins) != 0 {
		t.Fatal("failed LAN setup left pin")
	}
}

func TestLANHTTPOnlyPreservesApplicationAuthAndScheme(t *testing.T) {
	m := liveNetworkManager(t)
	backend := newExposureBackend(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s", r.Header.Get("X-Forwarded-Proto"), r.Header.Get("Authorization"))
	})
	ctx := context.Background()
	hp := freeNetworkPort(t)
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("0.0.0.0:%d", hp), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.AddGlobal(ctx, "app", backend.server.Listener.Addr().(*net.TCPAddr).Port, "app.localhost", "", "auto", tasks.ID()); e != nil {
		t.Fatal(e)
	}
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	result, e := m.ExposeStart(ctx, ExposureRequest{Route: st.GlobalRoute("app").Ref(), Provider: "lan", NoAuth: true, RequestID: tasks.ID()}, project.Context{}, nil, hosts.Manager{Data: m.Data, Isolated: true})
	if e != nil {
		t.Fatal(e)
	}
	item := result.(map[string]any)["item"].(expose.Public)
	request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", hp), nil)
	request.Host = strings.TrimPrefix(item.URL, "http://")
	request.Header.Set("Authorization", "Bearer dummy-app-token")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(body) != "http|Bearer dummy-app-token" {
		t.Fatal(response.Status, string(body), err)
	}
	if _, e := m.ExposeStop(ctx, item.ID, tasks.ID()); e != nil {
		t.Fatal(e)
	}
}
