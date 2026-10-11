package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestDNSMappingUsesReachableLoopbackForWildcardListeners(t *testing.T) {
	for _, tc := range []struct {
		name, host, dns, http, https, want, errorCode string
	}{
		{name: "IPv4 wildcard", host: "app.test", dns: "auto", http: "0.0.0.0", want: "127.0.0.1"},
		{name: "IPv6 wildcard", host: "app.test", dns: "hosts", http: "::", want: "::1"},
		{name: "IPv4 loopback", host: "app.test", dns: "auto", http: "127.0.0.2", want: "127.0.0.2"},
		{name: "IPv6 loopback", host: "app.test", dns: "auto", http: "::1", want: "::1"},
		{name: "HTTPS selected", host: "app.test", dns: "auto", http: "0.0.0.0", https: "::", want: "::1"},
		{name: "localhost unchanged", host: "app.localhost", dns: "auto", http: "0.0.0.0"},
		{name: "external unchanged", host: "app.test", dns: "external", http: "0.0.0.0"},
		{name: "specific remote rejected", host: "app.test", dns: "auto", http: "192.0.2.1", errorCode: "dns_address_required"},
		{name: "invalid rejected", host: "app.test", dns: "auto", http: "invalid", errorCode: "dns_address_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := network.Listener{HTTPHost: tc.http, HTTPSHost: tc.https}
			if tc.https != "" {
				listener.HTTPSReservation = "tls"
			}
			got, err := DNSMapping(tc.host, tc.dns, listener)
			if tc.errorCode != "" {
				if err == nil || err.Code != tc.errorCode {
					t.Fatal(got, err)
				}
				return
			}
			var want []hosts.Mapping
			if tc.want != "" {
				want = []hosts.Mapping{{Host: tc.host, Address: tc.want}}
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal(got, err, want)
			}
		})
	}
}

func TestGlobalRouteConnectsBusyBackendAndRetainsIdentity(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "backend") }))
	defer backend.Close()
	port := backend.Listener.Addr().(*net.TCPAddr).Port
	listen := freeNetworkPort(t)
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", listen), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	addID := tasks.ID()
	if _, e := m.AddGlobal(ctx, "app", port, "app.test", "", "external", addID); e != nil {
		t.Fatal(e)
	}
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	original := *st.GlobalRoute("app")
	hm := hosts.Manager{Data: m.Data}
	if _, e := m.RouteUp(ctx, original.Ref(), project.Context{}, nil, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d", listen), nil)
	request.Host = "app.test"
	client := &http.Client{Timeout: time.Second}
	res, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "backend" {
		t.Fatal(string(body))
	}
	if _, e := m.RouteDown(ctx, original.Ref(), hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	res, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatal(res.Status)
	}
	if _, e := m.RemoveGlobals(ctx, []string{"app"}, false, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.AddGlobal(ctx, "app", port, "app.test", "", "external", tasks.ID()); e != nil {
		t.Fatal(e)
	}
	st, e = m.portStore().Read()
	if e != nil || st.GlobalRoute("app").ID == original.ID {
		t.Fatal(st, e)
	}
	if _, e := m.RouteUp(ctx, original.Ref(), project.Context{}, nil, hm, tasks.ID()); e == nil || e.Code != "route_not_found" {
		t.Fatal(e)
	}
}
func TestProjectActivationFreezesEnvironmentAndDetectsSources(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	root := t.TempDir()
	config := "profile='app'\nenv_files=['.env']\n[ports.web]\nport=31000\n[proxies.app]\nhost='${DOMAIN}'\nport='web'\n"
	path := filepath.Join(root, "devtools.toml")
	if e := os.WriteFile(path, []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	envpath := filepath.Join(root, ".env")
	if e := os.WriteFile(envpath, []byte("DOMAIN=file.localhost\nUNUSED_PRIVATE=never-store-me\n"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	instance := register(t, m.portStore(), p.Profile, p.Root, nil, 31000)
	ref := network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: "app"}
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	hm := hosts.Manager{Data: m.Data, Isolated: true}
	result, e := m.RouteUp(ctx, ref, p, []string{"DOMAIN=parent.localhost", "UNUSED_PRIVATE=parent-secret"}, hm, tasks.ID())
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "source_digest") || strings.Contains(string(encoded), "never-store-me") {
		t.Fatal(string(encoded))
	}
	resolver := NewResolver(m.portStore())
	items, e := resolver.List(ctx, "")
	if e != nil || len(items) != 1 || items[0].Host == nil || *items[0].Host != "parent.localhost" || items[0].Status != StatusReady {
		t.Fatal(items, e)
	}
	t.Setenv("DOMAIN", "daemon-wrong.localhost")
	items, e = resolver.List(ctx, "")
	if e != nil || *items[0].Host != "parent.localhost" {
		t.Fatal(items, e)
	}
	if e := os.WriteFile(envpath, []byte("DOMAIN=changed.localhost\n"), 0600); e != nil {
		t.Fatal(e)
	}
	items, e = resolver.List(ctx, "")
	if e != nil || items[0].Status != "activation_required" {
		t.Fatal(items, e)
	}
	if _, e := m.RouteDown(ctx, ref, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	items, e = resolver.List(ctx, "")
	if e != nil || items[0].Status != "inactive" {
		t.Fatal(items, e)
	}
	registry, _ := os.ReadFile(filepath.Join(m.Data, "ports", "registry.json"))
	if strings.Contains(string(registry), "never-store-me") || strings.Contains(string(registry), "parent-secret") {
		t.Fatal("unreferenced secrets persisted")
	}
}
func TestCustomHostPreparationUsesInjectedHelperAndFailsClosed(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.AddGlobal(ctx, "app", 31001, "app.test", "", "auto", tasks.ID()); e != nil {
		t.Fatal(e)
	}
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	ref := st.GlobalRoute("app").Ref()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0600); err != nil {
		t.Fatal(err)
	}
	system := hosts.System{Path: path}
	calls := 0
	hm := hosts.Manager{Data: m.Data, System: system, Run: func(ctx context.Context, _ string, input any, _ bool) *protocol.Error {
		calls++
		return system.Apply(ctx, input.(hosts.Request))
	}}
	if _, e := m.RouteUp(ctx, ref, project.Context{}, nil, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	if _, e := m.RouteDown(ctx, ref, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	hm.Isolated = true
	if _, e := m.RouteUp(ctx, ref, project.Context{}, nil, hm, tasks.ID()); e == nil || e.Code != "isolated_operation_not_allowed" || calls != 2 {
		t.Fatal(e, calls)
	}
	st, e = m.portStore().Read()
	if e != nil || st.Activation(ref).Mode != "disabled" {
		t.Fatal(st, e)
	}
}
