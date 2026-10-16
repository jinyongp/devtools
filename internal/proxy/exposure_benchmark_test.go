package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func BenchmarkExposureRequest(b *testing.B) {
	for _, size := range []int{100, 1 << 20} {
		b.Run(fmt.Sprintf("env%d", size), func(b *testing.B) {
			resolver, _ := resolverSourcesFixture(b, 8, size)
			store := resolver.Ports
			ctx := context.Background()
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }))
			defer backend.Close()
			port := backend.Listener.Addr().(*net.TCPAddr).Port
			// Only the benchmark bridge binds a frontend socket. Registry listener state
			// supplies the same identity/pin checks as a running daemon.
			store.Probe = func(int) (bool, *protocol.Error) { return true, nil }
			listener, e := store.ConfigureListener(ctx, "default", "127.0.0.1:32080", "", false)
			if e != nil {
				b.Fatal(e)
			}
			if e := store.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
				st.Assignments[0].Port = port
				return true, nil
			}); e != nil {
				b.Fatal(e)
			}
			st, e := store.Read()
			if e != nil {
				b.Fatal(e)
			}
			ref := network.RouteRef{Scope: "project", Profile: "app", InstanceID: st.Instances[0].ID, Proxy: "r0"}
			pin := network.Pin{ID: tasks.ID(), Route: ref, Service: "web", ListenerID: listener.ID, ListenerGeneration: listener.Generation, NetworkDigest: project.Digest("fixture"), Port: port, Provider: "cloudflared"}
			if e := store.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
				st.Network.Pins = append(st.Network.Pins, pin)
				return true, nil
			}); e != nil {
				b.Fatal(e)
			}
			m := Manager{Data: filepath.Dir(store.Directory)}
			_, _, identity, e := m.exposureIdentity(ctx, ref)
			if e != nil {
				b.Fatal(e)
			}
			x := &exposureRuntime{record: expose.Record{Public: expose.Public{ID: pin.ID, Route: ref, State: "active", URL: "https://public.example"}, Identity: identity, Pin: pin}, transport: loopbackTransport(), owner: &networkRuntime{manager: m, groups: map[string]*listenerGroup{listener.ID: {record: Record{State: "running"}}}}}
			defer x.transport.CloseIdleConnections()
			front := httptest.NewServer(http.HandlerFunc(x.serve))
			defer front.Close()
			transport := &http.Transport{Proxy: nil}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			request, _ := http.NewRequest("GET", front.URL, nil)
			request.Host = "public.example"
			call := func() {
				response, e := client.Do(request)
				if e != nil {
					b.Fatal(e)
				}
				body, e := io.ReadAll(response.Body)
				response.Body.Close()
				if e != nil || response.StatusCode != 200 || string(body) != "ok" {
					b.Fatal(response.Status, string(body), e)
				}
			}
			call()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				call()
			}
		})
	}
}
