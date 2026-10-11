package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type fakeExposureProvider struct{ starts, stops atomic.Int32 }
type fakeExposureSession struct {
	parent *fakeExposureProvider
	live   atomic.Bool
}

func (p *fakeExposureProvider) Start(_ context.Context, _ expose.Record, origin string) (expose.ProviderSession, *protocol.Error) {
	p.starts.Add(1)
	request, _ := http.NewRequest("GET", origin, nil)
	request.Host = "public.example"
	response, e := http.DefaultClient.Do(request)
	if e != nil {
		return nil, expose.Failure("fake_start_failed")
	}
	response.Body.Close()
	if response.StatusCode != 503 {
		return nil, expose.Failure("guard_not_installed")
	}
	s := &fakeExposureSession{parent: p}
	s.live.Store(true)
	return s, nil
}
func (s *fakeExposureSession) URL() string                { return "https://public.example" }
func (s *fakeExposureSession) Owner() string              { return "fake-owned-session" }
func (s *fakeExposureSession) Alive(context.Context) bool { return s.live.Load() }
func (s *fakeExposureSession) Stop(context.Context) *protocol.Error {
	if s.live.Swap(false) {
		s.parent.stops.Add(1)
	}
	return nil
}

func TestExposureBridgeAuthPinsSourceRevocationAndSecretRedaction(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	provider := &fakeExposureProvider{}
	m.ExposureProviders["cloudflared"] = provider
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			fmt.Fprintln(w, "opened")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		fmt.Fprintf(w, "%s|%s|%s|%s", r.Header.Get("X-Forwarded-Proto"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("Forwarded"), r.Header.Get("Authorization"))
	}))
	defer backend.Close()
	port := backend.Listener.Addr().(*net.TCPAddr).Port
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	networkRoute(t, m, "app.localhost", "default", port)
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	instance := st.Instances[0]
	p, e := project.Resolve(instance.Directory, "")
	if e != nil {
		t.Fatal(e)
	}
	ref := network.RouteRef{Scope: "project", Profile: "app", InstanceID: instance.ID, Proxy: "app"}
	q := ExposureRequest{Route: ref, Provider: "cloudflared", Auth: "reader:private-canary", RequestID: tasks.ID()}
	result, e := m.ExposeStart(ctx, q, p, nil, hosts.Manager{Data: m.Data, Isolated: true})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(result)
	var decoded struct {
		Item expose.Public `json:"item"`
	}
	json.Unmarshal(b, &decoded)
	item := decoded.Item
	if item.State != "active" || item.URL != "https://public.example" || provider.starts.Load() != 1 {
		t.Fatal(string(b))
	}
	call := func(path, authority, auth string) *http.Response {
		t.Helper()
		r, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", item.OriginPort, path), nil)
		r.Host = authority
		r.Header.Set("X-Forwarded-Proto", "spoof")
		r.Header.Set("Forwarded", "for=spoof")
		if auth != "" {
			r.SetBasicAuth("reader", auth)
		}
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return response
	}
	response := call("/", "public.example", "")
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal(response.Status)
	}
	response = call("/", "wrong.example", "private-canary")
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal(response.Status)
	}
	response = call("/", "public.example", "private-canary")
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || string(body) != "https|public.example||" {
		t.Fatal(response.Status, string(body))
	}
	if _, e := m.RouteDown(ctx, ref, hosts.Manager{Data: m.Data}, tasks.ID()); e == nil || e.Code != "pinned" {
		t.Fatal("active pin not enforced", e)
	}
	if _, e := m.ExposeStart(ctx, q, p, nil, hosts.Manager{Data: m.Data}); e != nil {
		t.Fatal(e)
	}
	if provider.starts.Load() != 1 {
		t.Fatal("replay launched provider")
	}
	q.Auth = "reader:different"
	if _, e := m.ExposeStart(ctx, q, p, nil, hosts.Manager{Data: m.Data}); e == nil || e.Code != "request_conflict" {
		t.Fatal("auth change replayed", e)
	}
	response = call("/stream", "public.example", "private-canary")
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if line, e := reader.ReadString('\n'); e != nil || line != "opened\n" {
		t.Fatal(line, e)
	}
	config, _ := os.ReadFile(p.ConfigPath)
	if e := os.WriteFile(p.ConfigPath, append(config, []byte("\n# tracked source changed\n")...), 0600); e != nil {
		t.Fatal(e)
	}
	ended := make(chan bool, 1)
	go func() { _, e := reader.ReadString('\n'); ended <- e != nil }()
	select {
	case ok := <-ended:
		if !ok {
			t.Fatal("stream continued")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not revoke stream")
	}
	if _, e := m.ExposeStop(ctx, item.ID, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if provider.stops.Load() != 1 {
		t.Fatal("provider not stopped")
	}
	q.Auth = "reader:private-canary"
	q.RequestID = tasks.ID()
	result, e = m.ExposeStart(ctx, q, p, nil, hosts.Manager{Data: m.Data})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.stop(ctx); e != nil {
		t.Fatal(e)
	}
	replayed, e := m.ExposeStart(ctx, q, p, nil, hosts.Manager{Data: m.Data})
	if e != nil {
		t.Fatal(e)
	}
	replayBytes, _ := json.Marshal(replayed)
	if !strings.Contains(string(replayBytes), `"state":"auth_required"`) || provider.starts.Load() != 2 {
		t.Fatal("completed replay restored lost auth", string(replayBytes))
	}
	filepath.WalkDir(m.Data, func(path string, entry os.DirEntry, e error) error {
		if e == nil && !entry.IsDir() {
			data, _ := os.ReadFile(path)
			if strings.Contains(string(data), "private-canary") || strings.Contains(string(data), "reader:different") {
				t.Fatalf("authentication persisted in %s", path)
			}
		}
		return nil
	})
}
