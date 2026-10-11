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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
