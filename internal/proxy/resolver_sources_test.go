package proxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func resolverSourcesFixture(tb testing.TB, routes, size int) (Resolver, string) {
	tb.Helper()
	root := tb.TempDir()
	config := "profile='app'\nenv_files=['.env']\n[ports.web]\nport=31000\n"
	for i := 0; i < routes; i++ {
		config += fmt.Sprintf("[proxies.r%d]\nhost='${DOMAIN%d}'\nport='web'\n", i, i)
	}
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); e != nil {
		tb.Fatal(e)
	}
	envPath := filepath.Join(root, ".env")
	if e := os.WriteFile(envPath, []byte(strings.Repeat("#", size)), 0600); e != nil {
		tb.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		tb.Fatal(e)
	}
	source, e := p.NetworkSources()
	if e != nil {
		tb.Fatal(e)
	}
	store := ports.Store{Directory: filepath.Join(tb.TempDir(), "ports")}
	if e := store.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) {
		instance, e := st.Register(p.Profile, p.Root)
		if e != nil {
			return false, e
		}
		st.Assignments = append(st.Assignments, ports.Assignment{Instance: *instance, Name: "web", Port: 31000})
		for i := 0; i < routes; i++ {
			name := fmt.Sprintf("r%d", i)
			if e := st.SetActivation(network.Activation{Route: network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: name}, Mode: "active", EnvironmentBacked: true, Host: fmt.Sprintf("r%d.localhost", i), Listener: "default", DNS: "auto", ConfigDigest: project.Digest(p.Proxies[name]), SourceDigest: source, NetworkDigest: project.Digest("fixture")}); e != nil {
				return false, e
			}
		}
		return true, nil
	}); e != nil {
		tb.Fatal(e)
	}
	return NewResolver(store), envPath
}

func TestResolverSourceResultsExpireBetweenLists(t *testing.T) {
	resolver, path := resolverSourcesFixture(t, 8, 100)
	check := func(want string) {
		t.Helper()
		items, e := resolver.List(context.Background(), "")
		if e != nil || len(items) != 8 {
			t.Fatal(items, e)
		}
		for _, item := range items {
			if item.Status != want {
				t.Fatalf("route %v status %q, want %q", item.Proxy, item.Status, want)
			}
		}
	}
	check(StatusReady)
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	// Equal size and timestamp must not conceal changed contents.
	if e := os.WriteFile(path, []byte(strings.Repeat("!", 100)), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Chtimes(path, info.ModTime(), info.ModTime()); e != nil {
		t.Fatal(e)
	}
	check("activation_required")
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	check("activation_required")
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	check("activation_required")
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(strings.Repeat("#", 100)), 0600); e != nil {
		t.Fatal(e)
	}
	check(StatusReady)
}

func BenchmarkResolverEnvironmentSources(b *testing.B) {
	for _, routes := range []int{1, 8} {
		b.Run(fmt.Sprintf("routes%d", routes), func(b *testing.B) {
			resolver, _ := resolverSourcesFixture(b, routes, 1<<20)
			ctx := context.Background()
			if _, e := resolver.List(ctx, ""); e != nil {
				b.Fatal(e)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				items, e := resolver.List(ctx, "")
				if e != nil || len(items) != routes {
					b.Fatal(items, e)
				}
				for _, item := range items {
					if item.Status != StatusReady {
						b.Fatal(item)
					}
				}
			}
		})
	}
}
