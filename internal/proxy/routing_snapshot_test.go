package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/testfs"
)

func TestRoutingSnapshotKeepsListenerAndDuplicateSemantics(t *testing.T) {
	a := readyRoute("same.localhost", 32000)
	a.Listener = "one"
	b := readyRoute("same.localhost", 32001)
	b.Listener = "two"
	for _, test := range []struct {
		listener string
		count    int
		port     int
	}{{"one", 1, 32000}, {"two", 1, 32001}, {"", 2, 32001}, {"missing", 0, 0}} {
		d := newRoutingSnapshot([]Item{a, b}).routes[routeKey{test.listener, "same.localhost"}]
		if d.matches != test.count || d.matches == 1 && *d.item.TargetPort != test.port {
			t.Fatal(test, d)
		}
	}
	legacy := readyRoute("legacy.localhost", 32002)
	legacy.Listener = ""
	if newRoutingSnapshot([]Item{legacy}).routes[routeKey{"default", "legacy.localhost"}].matches != 1 {
		t.Fatal("legacy default listener lost")
	}
}

func TestRoutingLookupRefreshesRegistryConfigAndGlobalConflicts(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "app", "route.localhost")
	i := register(t, store, "app", directory, nil, 30530)
	r := NewResolver(store)
	h := NewHandler(r)
	h.Transport = benchmarkTransport{}
	request := func(host string) int {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil))
		return w.Code
	}
	if request("route.localhost") != http.StatusOK {
		t.Fatal("initial route unavailable")
	}
	listed, listErr := r.List(context.Background(), "")
	if listErr != nil || len(listed) != 1 {
		t.Fatal(listed, listErr)
	}
	*listed[0].Host, *listed[0].TargetPort = "mutated.localhost", 1
	if request("route.localhost") != http.StatusOK {
		t.Fatal("management output changed routing snapshot")
	}
	old, err := r.lookupRoute(context.Background(), "", "route.localhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) { st.Get(i.ID, "web").Port = 30531; return true, nil }); err != nil {
		t.Fatal(err)
	}
	current, err := r.lookupRoute(context.Background(), "", "route.localhost")
	if err != nil || *current.item.TargetPort != 30531 || *old.item.TargetPort != 30530 {
		t.Fatal("routing snapshots changed in place or missed update", current, err)
	}
	writeConfig(t, directory, "app", "new.localhost")
	if request("route.localhost") != http.StatusNotFound || request("new.localhost") != http.StatusOK {
		t.Fatal("config change missed")
	}
	other := t.TempDir()
	writeConfig(t, other, "other", "new.localhost")
	register(t, store, "other", other, nil, 30532)
	if request("new.localhost") != http.StatusServiceUnavailable {
		t.Fatal("global host conflict bypassed")
	}
}

func TestRoutingLookupRefreshesProfileNames(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(testfs.TempDir(t), "data")
	privateRoot, err := platformfs.OpenRoot(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := privateRoot.Close(); err != nil {
		t.Fatal(err)
	}
	store := ports.Store{Directory: filepath.Join(root, "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "renamed", "route.localhost")
	instance := register(t, store, "app", directory, nil, 30530)
	registry, failure := profilekey.ReadRegistry(root)
	if failure != nil {
		t.Fatal(failure)
	}
	profile, failure := registry.Register("app")
	if failure != nil {
		t.Fatal(failure)
	}
	if failure := registry.Publish(root); failure != nil {
		t.Fatal(failure)
	}
	resolver := NewResolver(store)
	before, failure := resolver.lookupRoute(ctx, "", "route.localhost")
	if failure != nil || before.matches != 0 {
		t.Fatal("unassigned name resolved to an instance", before, failure)
	}

	profile.Name = "renamed"
	registry.Names[profile.Name], registry.Profiles[profile.ID] = profile.ID, profile
	if failure := registry.Publish(root); failure != nil {
		t.Fatal(failure)
	}
	interrupted, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, failure := resolver.lookupRoute(cancelOnScan{interrupted, cancel}, "", "route.localhost"); failure == nil || failure.Code != "canceled" {
		t.Fatal("canceled route lookup did not stop before publishing a snapshot", failure)
	}
	after, failure := resolver.lookupRoute(ctx, "", "route.localhost")
	if failure != nil || after.matches != 1 || after.item.InstanceID != instance.ID || after.item.Profile != "app" {
		t.Fatal("profile name publication did not refresh routing to the original instance", after, failure)
	}
}

// Cancel at the first per-instance check, after the resolver reads its registries.
type cancelOnScan struct {
	context.Context
	cancel context.CancelFunc
}

func (c cancelOnScan) Err() error {
	c.cancel()
	return c.Context.Err()
}
