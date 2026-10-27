package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func benchmarkGlobalWithProjects(b *testing.B, count int) Resolver {
	b.Helper()
	root := b.TempDir()
	store := ports.Store{Directory: filepath.Join(root, "ports")}
	if e := store.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) {
		for i := 0; i < count; i++ {
			dir := filepath.Join(root, fmt.Sprintf("project%d", i))
			if err := os.Mkdir(dir, 0700); err != nil {
				b.Fatal(err)
			}
			content := fmt.Sprintf("profile='bench'\n[ports.web]\n[proxies.http]\nport='web'\nhost='p%d.localhost'\n", i)
			if err := os.WriteFile(filepath.Join(dir, "devtools.toml"), []byte(content), 0600); err != nil {
				b.Fatal(err)
			}
			instance := ports.Instance{Profile: "bench", ID: fmt.Sprintf("%032x", i+1), Directory: dir}
			st.Instances = append(st.Instances, instance)
			st.Assignments = append(st.Assignments, ports.Assignment{Instance: instance, Name: "web", Port: 20000 + i})
		}
		if e := st.EnsureDefaultListener(tasks.ID(), tasks.ID()); e != nil {
			return false, e
		}
		if _, e := st.Reference("global-backend", 32000); e != nil {
			return false, e
		}
		route := network.GlobalRoute{ID: tasks.ID(), Name: "global", Host: "global.localhost", Reservation: "global-backend", Listener: "default", DNS: "external"}
		st.Network.Routes = append(st.Network.Routes, route)
		e := st.SetActivation(network.Activation{Route: route.Ref(), Mode: "active", Host: route.Host, Listener: "default", DNS: "external", ConfigDigest: strings.Repeat("a", 64), SourceDigest: strings.Repeat("b", 64), NetworkDigest: strings.Repeat("c", 64)})
		return true, e
	}); e != nil {
		b.Fatal(e)
	}
	r := NewResolver(store)
	if _, e := r.List(context.Background(), ""); e != nil {
		b.Fatal(e)
	}
	return r
}

// The real resolver checks every registered source even for a global target.
// Transport is in-process; these measurements are not network latency.
func BenchmarkProxyGlobalFreshness(b *testing.B) {
	for _, count := range []int{0, 1, 50, 200} {
		for _, parallel := range []bool{false, true} {
			b.Run(fmt.Sprintf("projects%d/parallel%t", count, parallel), func(b *testing.B) {
				r := benchmarkGlobalWithProjects(b, count)
				h := NewHandler(r)
				h.Transport = benchmarkTransport{}
				request := func() {
					w := httptest.NewRecorder()
					h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://global.localhost/file.js", nil))
					if w.Code != 200 {
						b.Fatalf("response%d %s", w.Code, w.Body.String())
					}
				}
				request()
				b.ReportAllocs()
				b.ResetTimer()
				if parallel {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							request()
						}
					})
				} else {
					for b.Loop() {
						request()
					}
				}
			})
		}
	}
}

func BenchmarkProxyFreshnessStages(b *testing.B) {
	r := benchmarkGlobalWithProjects(b, 1)
	state, _, e := r.readPorts()
	if e != nil {
		b.Fatal(e)
	}
	instance := state.Instances[0]
	p, e, _ := r.resolveProject(instance)
	if e != nil {
		b.Fatal(e)
	}
	for _, stage := range []string{"registry_identity", "project_identity", "source_hash", "forward"} {
		b.Run(stage, func(b *testing.B) {
			h := NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
				return []Item{readyRoute("global.localhost", 32000)}, nil
			}))
			h.Transport = benchmarkTransport{}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				switch stage {
				case "registry_identity":
					if _, _, e := r.readPorts(); e != nil {
						b.Fatal(e)
					}
				case "project_identity":
					if _, e, _ := r.resolveProject(instance); e != nil {
						b.Fatal(e)
					}
				case "source_hash":
					if _, e := p.NetworkSources(); e != nil {
						b.Fatal(e)
					}
				case "forward":
					w := httptest.NewRecorder()
					h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://global.localhost/file.js", nil))
					if w.Code != 200 {
						b.Fatal(w.Code)
					}
				}
			}
		})
	}
	// This isolates the channel's scheduling floor, with no work held inside it.
	// Actual contended request time is measured by GlobalFreshness/paralleltrue.
	b.Run("list_gate_scheduling_floor", func(b *testing.B) {
		var wait atomic.Int64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				at := time.Now()
				r.cache.listGate <- struct{}{}
				wait.Add(time.Since(at).Nanoseconds())
				<-r.cache.listGate
			}
		})
		b.ReportMetric(float64(wait.Load())/float64(b.N), "wait-ns/op")
	})
}

func BenchmarkProxyChangedSources(b *testing.B) {
	for _, changed := range []bool{false, true} {
		b.Run(fmt.Sprintf("changed%t", changed), func(b *testing.B) {
			r, path := resolverSourcesFixture(b, 1, 1<<20)
			ctx := context.Background()
			info, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			original, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			modified := append([]byte(nil), original...)
			modified[0] = '!'
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				want := StatusReady
				if changed {
					b.StopTimer()
					body := original
					if n%2 == 0 {
						body = modified
						want = "activation_required"
					}
					if err := os.WriteFile(path, body, 0600); err != nil {
						b.Fatal(err)
					}
					if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				}
				items, e := r.List(ctx, "")
				if e != nil || len(items) != 1 || items[0].Status != want {
					b.Fatal(items, e)
				}
			}
		})
	}
}
