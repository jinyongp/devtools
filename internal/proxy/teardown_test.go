package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/truststore"
)

func TestSelectedTeardownPreservesSharedCAAndUntrustFailurePreservesKey(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	hm := hosts.Manager{Data: m.Data, Isolated: true}
	ports := map[string]int{}
	for _, name := range []string{"first", "second"} {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
		defer backend.Close()
		port := backend.Listener.Addr().(*net.TCPAddr).Port
		ports[name] = freeNetworkPort(t)
		if _, e := m.Configure(ctx, name, fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), fmt.Sprintf("127.0.0.1:%d", ports[name]), false, tasks.ID()); e != nil {
			t.Fatal(e)
		}
		if _, e := m.AddGlobal(ctx, name, port, name+".localhost", name, "auto", tasks.ID()); e != nil {
			t.Fatal(e)
		}
		st, _ := m.portStore().Read()
		if _, e := m.RouteUp(ctx, st.GlobalRoute(name).Ref(), project.Context{}, nil, hm, tasks.ID()); e != nil {
			t.Fatal(e)
		}
	}
	a, e := (ca.Store{Data: m.Data}).Read()
	if e != nil {
		t.Fatal(e)
	}
	fingerprint := a.Fingerprint()
	plan, e := m.TeardownPreview(ctx, TeardownSelection{Listener: "first"}, hm)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.Teardown(ctx, plan.Selection, plan.Digest, tasks.ID(), hm, TeardownTrust{}); e != nil {
		t.Fatal(e)
	}
	a, e = (ca.Store{Data: m.Data}).Read()
	if e != nil || a.Fingerprint() != fingerprint {
		t.Fatal("shared CA changed", e)
	}
	st, e := m.portStore().Read()
	if e != nil || st.Listener("first") != nil || st.Listener("second") == nil || st.GlobalRoute("first") == nil || st.Activation(st.GlobalRoute("first").Ref()).Mode != "disabled" {
		t.Fatal("selected teardown lost declaration or other listener", e)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(a.PEM())
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "second.localhost"}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	req, _ := http.NewRequest("GET", fmt.Sprintf("https://127.0.0.1:%d/", ports["second"]), nil)
	req.Host = "second.localhost"
	response, err := client.Do(req)
	if err != nil {
		t.Fatal("surviving TLS handshake failed", err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	plan, e = m.TeardownPreview(ctx, TeardownSelection{All: true, RemoveCA: true}, hm)
	if e != nil {
		t.Fatal(e)
	}
	id := tasks.ID()
	untrustCalls := 0
	trust := TeardownTrust{Untrust: func(context.Context, *ca.Authority) *protocol.Error {
		untrustCalls++
		if _, e := (ca.Store{Data: m.Data}).Read(); e != nil {
			t.Fatal("key deleted before untrust", e)
		}
		return protocol.NewError("permission_required", "authorization unavailable", 3, nil)
	}}
	if _, e := m.Teardown(ctx, plan.Selection, plan.Digest, id, hm, trust); e == nil || e.Code != "permission_required" {
		t.Fatal(e)
	}
	if _, e := (ca.Store{Data: m.Data}).Read(); e != nil {
		t.Fatal("failed untrust lost key", e)
	}
	trust.Untrust = func(context.Context, *ca.Authority) *protocol.Error { untrustCalls++; return nil }
	trust.Check = func(context.Context, *ca.Authority) (truststore.TrustStatus, *protocol.Error) {
		return truststore.TrustStatus{}, nil
	}
	if _, e := m.Teardown(ctx, plan.Selection, plan.Digest, id, hm, trust); e == nil || e.Code != "untrust_unconfirmed" {
		t.Fatal(e)
	}
	trust.Check = func(context.Context, *ca.Authority) (truststore.TrustStatus, *protocol.Error) {
		return truststore.TrustStatus{Known: true}, nil
	}
	if _, e := m.Teardown(ctx, plan.Selection, plan.Digest, id, hm, trust); e != nil {
		t.Fatal(e)
	}
	if _, e := (ca.Store{Data: m.Data}).Read(); e == nil || e.Code != "ca_not_found" || untrustCalls != 3 {
		t.Fatal("CA removal did not reconcile", untrustCalls, e)
	}
}

func TestTeardownRejectsDriftAndIsolatedSharedTrustMutation(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	hm := hosts.Manager{Data: m.Data, Isolated: true}
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	plan, e := m.TeardownPreview(ctx, TeardownSelection{All: true, RemoveCA: true}, hm)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.Configure(ctx, "new", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Teardown(ctx, plan.Selection, plan.Digest, tasks.ID(), hm, TeardownTrust{}); e == nil || e.Code != "stale_preview" {
		t.Fatal(e)
	}
	m.IsolatedRoot = t.TempDir()
	if _, e := m.Teardown(ctx, plan.Selection, plan.Digest, tasks.ID(), hm, TeardownTrust{}); e == nil || e.Code != "isolated_operation_not_allowed" {
		t.Fatal(e)
	}
}
