package proxy

import (
	"context"
	"fmt"
	"testing"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestBatchPreparationReusesPinnedActivationAndRestartsStoppedListener(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	p, ref := networkRoute(t, m, "batch.localhost", "default", 31001)
	hm := hosts.Manager{Data: m.Data, Isolated: true}
	if _, e := m.RouteUp(ctx, ref, p, nil, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	a := *st.Activation(ref)
	if _, e := m.RouteDown(ctx, ref, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if e := m.ActivateProjectBatch(ctx, p, []network.Activation{a}, hm, tasks.ID(), true); e == nil || e.Code != "network_snapshot_changed" {
		t.Fatal(e)
	}
	if e := m.ActivateProjectBatch(ctx, p, []network.Activation{a}, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if _, e := m.NetworkApply(ctx, "stop", "default", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if e := m.ActivateProjectBatch(ctx, p, []network.Activation{a}, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	status, e := m.NetworkStatus(ctx, "default")
	if e != nil || status["running"] != true {
		t.Fatal(status, e)
	}
	if e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		l := st.Listener("default")
		st.Network.Pins = append(st.Network.Pins, network.Pin{ID: tasks.ID(), Route: ref, Service: "web", ListenerID: l.ID, ListenerGeneration: l.Generation, NetworkDigest: a.NetworkDigest, Port: 31001, Provider: "cloudflared"})
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := m.ActivateProjectBatch(ctx, p, []network.Activation{a}, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	a.Host = "changed.localhost"
	if e := m.ActivateProjectBatch(ctx, p, []network.Activation{a}, hm, tasks.ID()); e == nil || e.Code != "pinned" {
		t.Fatal(e)
	}
}

func TestBatchPreparationPreservesPinnedImplicitRoute(t *testing.T) {
	m := liveNetworkManager(t)
	ctx := context.Background()
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	p, ref := networkRoute(t, m, "implicit.localhost", "default", 31002)
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	source, e := p.NetworkSources()
	if e != nil {
		t.Fatal(e)
	}
	a, e := BuildProjectActivation(st, ref, p.Proxies["app"], "implicit.localhost", source, false)
	if e != nil {
		t.Fatal(e)
	}
	if e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		l := st.Listener("default")
		st.Network.Pins = append(st.Network.Pins, network.Pin{ID: tasks.ID(), Route: ref, Service: "web", ListenerID: l.ID, ListenerGeneration: l.Generation, NetworkDigest: a.NetworkDigest, Port: 31002, Provider: "cloudflared"})
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := m.ActivateProjectBatch(ctx, p, []network.Activation{a}, hosts.Manager{Data: m.Data, Isolated: true}, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	st, e = m.portStore().Read()
	if e != nil || st.Activation(ref) != nil || !st.ActivationMatches(a) {
		t.Fatal(st, e)
	}
}
