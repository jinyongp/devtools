package ports

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestNetworkUpgradePreservesLegacyRegistryAndReadBytes(t *testing.T) {
	s := testPortStore(t)
	ctx := context.Background()
	if _, _, e := s.Reserve(ctx, "proxy", 32000); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(s.Directory, "registry.json"))
	if st, e := s.Read(); e != nil || st.Version != registryVersion {
		t.Fatal(st, e)
	}
	after, _ := os.ReadFile(filepath.Join(s.Directory, "registry.json"))
	if string(before) != string(after) {
		t.Fatal("read rewrote registry")
	}
	l, e := s.ConfigureListener(ctx, "default", "", "127.0.0.1:32443", false)
	if e != nil {
		t.Fatal(e)
	}
	st, e := s.Read()
	if e != nil || st.Version != registryVersion || l.HTTPReservation != "proxy" || st.Reservation("proxy").Port != 32000 {
		t.Fatal(st, e)
	}
	if h, tls, e := st.ListenerPorts(""); e != nil || h != 32000 || tls != 32443 {
		t.Fatal(h, tls, e)
	}
}

func TestListenerConfigurationRollsBackEntireUnionOnSecondBindFailure(t *testing.T) {
	s := testPortStore(t)
	ctx := context.Background()
	if _, _, e := s.Reserve(ctx, "proxy", 32000); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(s.Directory, "registry.json"))
	s.Probe = func(port int) (bool, *protocol.Error) { return port != 32443, nil }
	if _, e := s.ConfigureListener(ctx, "preview", "127.0.0.1:32080", "127.0.0.1:32443", false); e == nil || e.Code != "port_in_use" {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(filepath.Join(s.Directory, "registry.json"))
	if string(before) != string(after) {
		t.Fatal("partial configuration saved")
	}
}

func TestFixedBackendReferenceDoesNotProbeExternalProcess(t *testing.T) {
	s := testPortStore(t)
	s.Probe = func(int) (bool, *protocol.Error) { t.Fatal("backend reference probed OS"); return false, nil }
	e := s.Update(context.Background(), func(st *State) (bool, *protocol.Error) {
		st.UpgradeNetwork()
		return st.Reference("backend.test", 32123)
	})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.Update(context.Background(), func(st *State) (bool, *protocol.Error) { return st.Reference("backend.other", 32123) }); e == nil || e.Code != "port_in_use" {
		t.Fatal(e)
	}
}

func TestPinnedAliasAndPortMutationPreserveStorage(t *testing.T) {
	s := testPortStore(t)
	ctx := context.Background()
	l, e := s.ConfigureListener(ctx, "default", "127.0.0.1:32080", "", false)
	if e != nil {
		t.Fatal(e)
	}
	e = s.Update(ctx, func(st *State) (bool, *protocol.Error) {
		i, e := st.Register("app", t.TempDir())
		if e != nil {
			return false, e
		}
		st.Assignments = append(st.Assignments, Assignment{Instance: *i, Name: "web", Port: 32123})
		st.Network.Pins = append(st.Network.Pins, network.Pin{ID: tasks.ID(), Route: network.RouteRef{Scope: "project", Profile: i.Profile, InstanceID: i.ID, Proxy: "app"}, Service: "web", ListenerID: l.ID, ListenerGeneration: l.Generation, NetworkDigest: strings.Repeat("a", 64), Port: 32123, Provider: "lan"})
		return true, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(s.Directory, "registry.json"))
	for _, mutation := range []func(*State){func(st *State) { alias := "changed"; st.Instances[0].Alias = &alias }, func(st *State) { st.Assignments[0].Port = 32124 }} {
		e = s.Update(ctx, func(st *State) (bool, *protocol.Error) { mutation(st); return true, nil })
		if e == nil || e.Code != "pinned" {
			t.Fatal(e)
		}
		after, _ := os.ReadFile(filepath.Join(s.Directory, "registry.json"))
		if string(before) != string(after) {
			t.Fatal("pinned mutation saved")
		}
	}
}

func TestGlobalRecreationHasNewIdentityAndDoesNotReuseTombstone(t *testing.T) {
	s := testPortStore(t)
	ctx := context.Background()
	first := tasks.ID()
	second := tasks.ID()
	add := func(id string) *protocol.Error {
		return s.Update(ctx, func(st *State) (bool, *protocol.Error) {
			st.UpgradeNetwork()
			key := GlobalReservation(id)
			if _, e := st.Reference(key, 32123); e != nil {
				return false, e
			}
			r := network.GlobalRoute{ID: id, Name: "app", Host: "app.localhost", Reservation: key, Listener: "default", DNS: "auto"}
			st.Network.Routes = append(st.Network.Routes, r)
			if e := st.SetActivation(network.Activation{Route: r.Ref(), Mode: "disabled"}); e != nil {
				return false, e
			}
			return true, nil
		})
	}
	if e := add(first); e != nil {
		t.Fatal(e)
	}
	if e := s.Update(ctx, func(st *State) (bool, *protocol.Error) { return true, st.RemoveGlobal("app") }); e != nil {
		t.Fatal(e)
	}
	if e := add(second); e != nil {
		t.Fatal(e)
	}
	st, e := s.Read()
	if e != nil || st.GlobalRoute("app").ID != second || st.Activation(network.RouteRef{Scope: "global", RouteID: first}) != nil {
		t.Fatal(st, e)
	}
}

func TestRegistryRejectsMalformedNetworkAndFutureVersion(t *testing.T) {
	s := testPortStore(t)
	if _, e := s.ConfigureListener(context.Background(), "default", "127.0.0.1:32080", "", false); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(s.Directory, "registry.json")
	data, _ := os.ReadFile(path)
	for _, edit := range []func(map[string]any){func(v map[string]any) { v["version"] = registryVersion + 1 }, func(v map[string]any) { v["network"].(map[string]any)["unknown"] = true }, func(v map[string]any) { v["network"].(map[string]any)["listeners"] = nil }} {
		var v map[string]any
		if e := json.Unmarshal(data, &v); e != nil {
			t.Fatal(e)
		}
		edit(v)
		b, _ := json.Marshal(v)
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Read(); e == nil || e.Code != "invalid_storage" {
			t.Fatal(e)
		}
	}
}

func TestSelectedPeerUnionResolvesFreshBindingsAndFailureSavesNothing(t *testing.T) {
	s := testPortStore(t)
	p := project.Context{Profile: "app", Root: t.TempDir(), Ports: map[string]project.Port{"web": {}, "api": {}}}
	web := project.Command{Serve: []string{"web"}, Bind: map[string]project.Binding{"API_PORT": {Port: "api"}}}
	api := project.Command{Serve: []string{"api"}}
	check := func(st *State) *protocol.Error { _, e := st.Bind(p, web, "", nil); return e }
	if _, e := s.AllocateUnion(context.Background(), p, []project.Command{web}, []int{32100, 32110}, check); e == nil || e.Code != "port_not_found" {
		t.Fatal(e)
	}
	st, e := s.Read()
	if e != nil || len(st.Instances) != 0 || len(st.Assignments) != 0 {
		t.Fatal(st, e)
	}
	a, e := s.AllocateUnion(context.Background(), p, []project.Command{web, api}, []int{32100, 32110}, check)
	if e != nil || len(a) != 2 {
		t.Fatal(a, e)
	}
	st, e = s.Read()
	if e != nil || len(st.Assignments) != 2 {
		t.Fatal(st, e)
	}
}
