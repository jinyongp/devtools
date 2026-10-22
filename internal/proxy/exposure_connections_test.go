package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

type exposureBackend struct {
	server         *httptest.Server
	accepted, open atomic.Int32
}

func newExposureBackend(t *testing.T, handler http.HandlerFunc) *exposureBackend {
	t.Helper()
	b := &exposureBackend{}
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, r.Header.Get("Authorization"))
		}
	}
	b.server = httptest.NewUnstartedServer(handler)
	b.server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			b.accepted.Add(1)
			b.open.Add(1)
		case http.StateClosed:
			b.open.Add(-1)
		}
	}
	b.server.Start()
	t.Cleanup(b.server.Close)
	return b
}

func (b *exposureBackend) waitOpen(t *testing.T, want int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for b.open.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("backend open connections = %d, want %d", b.open.Load(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExposureReusesAndClosesOnlyItsBackendPool(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprintf("control_port_collision=%t", collision), func(t *testing.T) {
			testExposureBackendPools(t, collision)
		})
	}
}

func testExposureBackendPools(t *testing.T, collision bool) {
	t.Helper()
	first, second := newExposureBackend(t, nil), newExposureBackend(t, nil)
	listener := freeNetworkPort(t)
	var attempts atomic.Int32
	var controlListen func() (net.Listener, error)
	if collision {
		controlListen = func() (net.Listener, error) {
			address := "127.0.0.1:0"
			if attempts.Add(1) == 1 {
				// The OS can choose a configured but not yet bound frontend
				// port when allocating the daemon's ephemeral control socket.
				address = fmt.Sprintf("127.0.0.1:%d", listener)
			}
			return net.Listen("tcp4", address)
		}
	}
	m := liveNetworkManagerWithControlListener(t, controlListen)
	m.ExposureProviders["cloudflared"] = &fakeExposureProvider{}
	ctx := context.Background()
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", listener), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	shares := make([]expose.Public, 0, 2)
	for i, backend := range []*exposureBackend{first, second} {
		name := fmt.Sprintf("app%d", i)
		if _, e := m.AddGlobal(ctx, name, backend.server.Listener.Addr().(*net.TCPAddr).Port, name+".localhost", "", "auto", tasks.ID()); e != nil {
			t.Fatal(e)
		}
		st, e := m.portStore().Read()
		if e != nil {
			t.Fatal(e)
		}
		result, e := m.ExposeStart(ctx, ExposureRequest{Route: st.GlobalRoute(name).Ref(), Provider: "cloudflared", NoAuth: true, RequestID: tasks.ID()}, project.Context{}, nil, hosts.Manager{Data: m.Data, Isolated: true})
		if e != nil {
			var control proxyControl
			controlErr := tasks.ReadPrivate(m.path("control.json"), &control)
			record, recordErr := m.readRecord()
			// Select metadata explicitly: the control token must never be logged.
			t.Fatalf("start exposure %s: code=%s details=%v frontend_port=%d control_address=%q control_read_ok=%t daemon_state=%q daemon_reason=%q daemon_read_ok=%t", name, e.Code, e.Details, listener, control.Address, controlErr == nil, record.State, record.Reason, recordErr == nil)
		}
		shares = append(shares, result.(map[string]any)["item"].(expose.Public))
	}
	if collision && attempts.Load() < 2 {
		t.Fatal("control endpoint did not reject the configured frontend port")
	}
	// Each frontend request uses a fresh socket; backend keep-alive is independent.
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	call := func(port int, host, authorization string) {
		t.Helper()
		request, e := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
		if e != nil {
			t.Fatal(e)
		}
		request.Host = host
		request.Header.Set("Authorization", authorization)
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || response.StatusCode != 200 || string(body) != authorization {
			t.Fatalf("status=%s body=%q error=%v", response.Status, body, e)
		}
	}
	for i := 0; i < 20; i++ {
		call(listener, "app0.localhost", "Bearer application-token")
		for _, share := range shares {
			call(share.OriginPort, "public.example", "Bearer application-token")
		}
	}
	// No-auth must preserve application Basic credentials as well as Bearer tokens.
	call(shares[0].OriginPort, "public.example", "Basic YXBwOnBhc3M=")
	if first.accepted.Load() != 2 || second.accepted.Load() != 1 {
		t.Fatalf("backend connections = %d, %d; want one local pool and one per exposure", first.accepted.Load(), second.accepted.Load())
	}
	if _, e := m.ExposeStop(ctx, shares[0].ID, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	first.waitOpen(t, 1)
	second.waitOpen(t, 1)
	call(listener, "app0.localhost", "Bearer application-token")
	call(shares[1].OriginPort, "public.example", "Bearer application-token")
	if first.accepted.Load() != 2 || second.accepted.Load() != 1 {
		t.Fatal("stopping one exposure disturbed a surviving pool")
	}
	if _, e := m.ExposeStop(ctx, shares[1].ID, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	second.waitOpen(t, 0)
}

type failedExposureStop struct{ fakeExposureSession }

func (*failedExposureStop) Stop(context.Context) *protocol.Error {
	return expose.Failure("provider_recovery_required")
}

func TestExposureStopClosesBackendPoolOnFailure(t *testing.T) {
	for _, failure := range []string{"storage", "provider", "daemon"} {
		t.Run(failure, func(t *testing.T) {
			backend := newExposureBackend(t, nil)
			data := t.TempDir()
			ref := network.RouteRef{Scope: "global", RouteID: tasks.ID()}
			record := expose.Record{Public: expose.Public{ID: tasks.ID(), Generation: tasks.ID(), Route: ref, State: "active"}, Pin: network.Pin{ID: tasks.ID(), Route: ref, ListenerID: tasks.ID(), ListenerGeneration: tasks.ID(), NetworkDigest: project.Digest("fixture"), Port: backend.server.Listener.Addr().(*net.TCPAddr).Port, Provider: "cloudflared"}}
			x := &exposureRuntime{record: record, transport: loopbackTransport(), owner: &networkRuntime{manager: Manager{Data: data}}}
			defer x.transport.CloseIdleConnections()
			store := expose.Store{Data: data}
			if e := store.Write(record); e != nil {
				t.Fatal(e)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "http://app.localhost/", nil)
			handler := Handler{Transport: x.transport}
			handler.forward(response, request, record.Pin.Port, record.Pin.HostPolicy)
			if response.Code != 200 {
				t.Fatal(response.Code)
			}
			backend.waitOpen(t, 1)
			reason := "stop_requested"
			wantError := ""
			switch failure {
			case "storage":
				// A directory at the record path forces atomic persistence to fail.
				if e := os.Remove(store.Path(record.ID)); e != nil {
					t.Fatal(e)
				}
				if e := os.Mkdir(store.Path(record.ID), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(store.Path(record.ID), "blocker"), nil, 0600); e != nil {
					t.Fatal(e)
				}
				wantError = "io_error"
			case "provider":
				x.provider = &failedExposureStop{}
				wantError = "provider_recovery_required"
			case "daemon":
				reason = "daemon_stopped"
			}
			_, e := x.stop(reason)
			if wantError == "" && e != nil || wantError != "" && (e == nil || e.Code != wantError) {
				t.Fatal(e, wantError)
			}
			backend.waitOpen(t, 0)
			if failure == "provider" {
				stored, e := store.Read(record.ID)
				if e != nil || stored.State != "recovery_required" {
					t.Fatal(stored, e)
				}
			}
		})
	}
}
