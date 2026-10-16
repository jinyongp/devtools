package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type busyExposureProvider struct {
	data       string
	failCommit bool
	session    *busyExposureSession
	prepareErr error
}
type busyExposureSession struct {
	provider   *busyExposureProvider
	origin, id string
	client     *http.Client
	transport  *http.Transport
	requests   sync.WaitGroup
}

func (p *busyExposureProvider) Start(_ context.Context, record expose.Record, origin string) (expose.ProviderSession, *protocol.Error) {
	tr := &http.Transport{Proxy: nil}
	s := &busyExposureSession{provider: p, origin: origin, id: record.ID, transport: tr, client: &http.Client{Transport: tr, Timeout: time.Second}}
	p.session = s
	req, _ := http.NewRequest("GET", origin, nil)
	req.Host = "public.example"
	response, e := s.client.Do(req)
	if e != nil {
		return nil, expose.Failure("start_probe_failed")
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		return nil, expose.Failure("precommit_ingress_open")
	}
	return s, nil
}
func (s *busyExposureSession) URL() string { return "https://public.example" }
func (s *busyExposureSession) Owner() string {
	if s.provider.failCommit {
		path := (expose.Store{Data: s.provider.data}).Path(s.id)
		if e := os.Remove(path); e != nil {
			s.provider.prepareErr = e
			return "busy-owner"
		}
		if e := os.Mkdir(path, 0700); e != nil {
			s.provider.prepareErr = e
			return "busy-owner"
		}
		if e := os.WriteFile(filepath.Join(path, "blocker"), nil, 0600); e != nil {
			s.provider.prepareErr = e
			return "busy-owner"
		}
	}
	started := make(chan struct{}, 4)
	for n := 0; n < 4; n++ {
		s.requests.Add(1)
		go func() {
			defer s.requests.Done()
			for i := 0; i < 40; i++ {
				req, _ := http.NewRequest("GET", s.origin, nil)
				req.Host = "public.example"
				response, e := s.client.Do(req)
				if e == nil {
					io.Copy(io.Discard, response.Body)
					response.Body.Close()
				}
				if i == 0 {
					started <- struct{}{}
				}
			}
		}()
	}
	for n := 0; n < 4; n++ {
		<-started
	}
	return "busy-owner"
}
func (s *busyExposureSession) Alive(context.Context) bool { return true }
func (s *busyExposureSession) Stop(context.Context) *protocol.Error {
	s.requests.Wait()
	s.transport.CloseIdleConnections()
	return nil
}

func TestExposureStartPublishesOnlyCommittedState(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		failCommit  bool
	}{{"new", "starting", false}, {"previously active", "active", false}, {"commit failure", "starting", true}} {
		t.Run(tc.name, func(t *testing.T) {
			resolver, _ := resolverSourcesFixture(t, 1, 100)
			store := resolver.Ports
			m := Manager{Data: filepath.Dir(store.Directory), ExposureProviders: map[string]expose.Provider{}}
			id := tasks.ID()
			var uncommitted atomic.Bool
			backend := newExposureBackend(t, func(w http.ResponseWriter, _ *http.Request) {
				record, e := (expose.Store{Data: m.Data}).Read(id)
				if e != nil || record.State != "active" {
					uncommitted.Store(true)
				}
				io.WriteString(w, "ok")
			})
			port := backend.server.Listener.Addr().(*net.TCPAddr).Port
			ctx := context.Background()
			listener, e := store.ConfigureListener(ctx, "default", net.JoinHostPort("127.0.0.1", itoa(freeNetworkPort(t))), "", false)
			if e != nil {
				t.Fatal(e)
			}
			if e := store.Update(ctx, func(st *ports.State) (bool, *protocol.Error) { st.Assignments[0].Port = port; return true, nil }); e != nil {
				t.Fatal(e)
			}
			st, e := store.Read()
			if e != nil {
				t.Fatal(e)
			}
			ref := st.Network.Activations[0].Route
			_, _, identity, e := m.exposureIdentity(ctx, ref)
			if e != nil {
				t.Fatal(e)
			}
			pin := network.Pin{ID: id, Route: ref, Service: "web", ListenerID: listener.ID, ListenerGeneration: listener.Generation, NetworkDigest: project.Digest("fixture"), Port: port, Provider: "cloudflared"}
			if e := store.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
				st.Network.Pins = append(st.Network.Pins, pin)
				return true, nil
			}); e != nil {
				t.Fatal(e)
			}
			record := expose.Record{Public: expose.Public{ID: id, Generation: tasks.ID(), Route: ref, State: tc.state, Provider: "cloudflared", URL: "https://public.example", OriginPort: freeNetworkPort(t)}, Identity: identity, Pin: pin}
			if e := (expose.Store{Data: m.Data}).Write(record); e != nil {
				t.Fatal(e)
			}
			provider := &busyExposureProvider{data: m.Data, failCommit: tc.failCommit}
			m.ExposureProviders["cloudflared"] = provider
			runtime := &networkRuntime{ctx: ctx, manager: m, attemptID: tasks.ID(), resolver: resolver, groups: map[string]*listenerGroup{listener.ID: {record: Record{State: "running"}}}, exposures: map[string]*exposureRuntime{}}
			t.Cleanup(func() {
				for _, x := range runtime.exposures {
					_, _ = x.stop("daemon_stopped")
				}
				if provider.session != nil {
					_ = provider.session.Stop(ctx)
				}
			})
			result, e := runtime.exposure(exposureRPCRequest{Action: "start", ID: id, Generation: record.Generation})
			if provider.prepareErr != nil {
				t.Fatal(provider.prepareErr)
			}
			if tc.failCommit {
				if e == nil || e.Code != "io_error" || result.State == "active" {
					t.Fatal("commit failure exposed an active result", result, e)
				}
			} else {
				if e != nil || result.State != "active" {
					t.Fatal(result, e)
				}
			}
			if provider.session != nil {
				provider.session.requests.Wait()
			}
			if uncommitted.Load() {
				t.Fatal("request reached backend before durable activation")
			}
			if tc.failCommit && backend.accepted.Load() != 0 {
				t.Fatal("failed start contacted backend")
			}
		})
	}
}
