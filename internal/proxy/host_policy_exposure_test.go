package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestHostPolicyExposureProviderAuthorityAndAuth(t *testing.T) {
	for _, provider := range []string{"lan", "cloudflared", "tailscale"} {
		for _, policy := range []network.HostPolicy{"", network.HostPreserve} {
			for _, auth := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/auth=%t", provider, policy.Effective(), auth), func(t *testing.T) {
					m := liveNetworkManager(t)
					m.ExposureProviders[provider] = &fakeExposureProvider{}
					backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						fmt.Fprintf(w, "%s|%s|%s|%s|%s|%s", r.Host, r.Header.Get("Origin"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto"), r.Header.Get("Forwarded"), r.Header.Get("Authorization"))
					}))
					defer backend.Close()
					ctx := context.Background()
					if _, e := m.Configure(ctx, "default", fmt.Sprintf("0.0.0.0:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
						t.Fatal(e)
					}
					if _, e := m.AddGlobalWithHostPolicy(ctx, "app", targetPort(t, backend), "app.localhost", "default", "auto", policy, tasks.ID()); e != nil {
						t.Fatal(e)
					}
					st, e := m.portStore().Read()
					if e != nil {
						t.Fatal(e)
					}
					ref := st.GlobalRoute("app").Ref()
					q := ExposureRequest{Route: ref, Provider: provider, NoAuth: !auth, RequestID: tasks.ID()}
					if auth {
						q.Auth = "reader:fixture-password"
					}
					result, e := m.ExposeStart(ctx, q, project.Context{}, nil, hosts.Manager{Data: m.Data, Isolated: true})
					if e != nil {
						t.Fatalf("start: %s: %v", e.Code, e.Details)
					}
					item := result.(map[string]any)["item"].(expose.Public)
					defer m.ExposeStop(ctx, item.ID, tasks.ID())
					u, _ := url.Parse(item.URL)
					port := item.OriginPort
					if provider == "lan" {
						state, _ := m.portStore().Read()
						port, _, _ = state.ListenerPorts("default")
					}
					req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
					req.Host = u.Host
					req.Header.Set("Origin", item.URL)
					req.Header.Set("X-Forwarded-Host", "spoof.invalid")
					req.Header.Set("X-Forwarded-Proto", "spoof")
					req.Header.Set("Forwarded", "host=spoof.invalid")
					wantAuth := "Bearer fixture-application-token"
					if auth {
						req.SetBasicAuth("reader", "fixture-password")
						wantAuth = ""
					} else {
						req.Header.Set("Authorization", wantAuth)
					}
					transport := &http.Transport{Proxy: nil}
					defer transport.CloseIdleConnections()
					client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
					response, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body, _ := io.ReadAll(response.Body)
					response.Body.Close()
					wantHost := backend.Listener.Addr().String()
					if policy == network.HostPreserve {
						wantHost = u.Host
					}
					want := strings.Join([]string{wantHost, item.URL, u.Host, u.Scheme, "", wantAuth}, "|")
					if response.StatusCode != 200 || string(body) != want {
						t.Fatalf("%s %q want %q", response.Status, body, want)
					}
					record, e := (expose.Store{Data: m.Data}).Read(item.ID)
					if e != nil || record.Pin.HostPolicy != policy.Canonical() {
						t.Fatal("policy not pinned", record.Pin, e)
					}
					if _, e := m.ExposeStop(ctx, item.ID, tasks.ID()); e != nil {
						t.Fatal(e)
					}
					record, e = (expose.Store{Data: m.Data}).Read(item.ID)
					if e != nil || record.State != "stopped" || record.Pin.HostPolicy != "" {
						t.Fatal("stopped policy metadata prevents downgrade", record, e)
					}
				})
			}
		}
	}
}

func TestHostPolicyChangeRevokesExposureAndRejectsOldRequest(t *testing.T) {
	m := liveNetworkManager(t)
	m.ExposureProviders["cloudflared"] = &fakeExposureProvider{}
	ctx := context.Background()
	backend := newExposureBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			fmt.Fprintln(w, "opened")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "alive")
	})
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	p, ref := networkRoute(t, m, "app.localhost", "default", targetPort(t, backend.server))
	original, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigPath, append(original, []byte("host_policy='preserve'\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	p, e := project.Resolve(p.Root, "")
	if e != nil {
		t.Fatal(e)
	}
	q := ExposureRequest{Route: ref, Provider: "cloudflared", NoAuth: true, RequestID: tasks.ID()}
	result, e := m.ExposeStart(ctx, q, p, nil, hosts.Manager{Data: m.Data, Isolated: true})
	if e != nil {
		t.Fatal(e)
	}
	item := result.(map[string]any)["item"].(expose.Public)
	defer m.ExposeStop(ctx, item.ID, tasks.ID())
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/stream", item.OriginPort), nil)
	req.Host = "public.example"
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	first := make([]byte, len("opened\n"))
	if _, err := io.ReadFull(response.Body, first); err != nil || string(first) != "opened\n" {
		t.Fatal(string(first), err)
	}
	ended := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, response.Body); ended <- err }()
	if err := os.WriteFile(p.ConfigPath, append(original, []byte("host_policy='rewrite'\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	changed, e := project.Resolve(p.Root, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.ExposeStart(ctx, q, changed, nil, hosts.Manager{Data: m.Data, Isolated: true}); e == nil || e.Code != "request_conflict" {
		t.Fatal("policy change replayed an old start", e)
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("policy change left the old generation streaming")
	}
	backend.waitOpen(t, 0)
}
