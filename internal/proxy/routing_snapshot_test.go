package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
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
