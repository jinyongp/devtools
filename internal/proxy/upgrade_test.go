package proxy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type upgradeRecoveryProvider struct {
	fakeExposureProvider
	available atomic.Bool
}

func (p *upgradeRecoveryProvider) RecoverStop(context.Context, expose.Record) *protocol.Error {
	if !p.available.Load() {
		return expose.Failure("provider_recovery_required")
	}
	return nil
}

func TestUpgradeReplacesDaemonAndRestoresListenersWithAbandonedPins(t *testing.T) {
	data := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	var daemons sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		daemons.Wait()
	})
	manager := func(build string) Manager {
		return Manager{Build: build}
	}
	old, current := manager("0.40.0/old"), manager("0.45.0/new")
	old.Data, current.Data = data, data
	provider := &upgradeRecoveryProvider{}
	current.ExposureProviders = map[string]expose.Provider{"cloudflared": provider}
	spawn := func(m Manager) func(string, string) error {
		return func(_ string, attempt string) error {
			daemons.Add(1)
			go func() {
				defer daemons.Done()
				_ = m.Serve(ctx, attempt)
			}()
			return nil
		}
	}
	old.Spawn, current.Spawn = spawn(old), spawn(current)
	if _, e := old.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	p, route := networkRoute(t, old, "app.localhost", "default", freeNetworkPort(t))
	hm := hosts.Manager{Data: old.Data, Isolated: true}
	if _, e := old.RouteUp(ctx, route, p, nil, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	before, err := current.rpc(ctx, "status")
	if err != nil || before.Build != old.Build {
		t.Fatal(before, err)
	}
	id := tasks.ID()
	record := expose.Record{Public: expose.Public{ID: id, Generation: tasks.ID(), Route: route, Provider: "lan", Listener: "default", State: "starting"}}
	if e := old.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		listener := st.Listener("default")
		record.Pin = network.Pin{ID: id, Route: route, ListenerID: listener.ID, ListenerGeneration: listener.Generation, NetworkDigest: strings.Repeat("a", 64), Port: st.Get(route.InstanceID, "web").Port, Provider: "lan", Service: "web"}
		st.Network.Pins = append(st.Network.Pins, record.Pin)
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := (expose.Store{Data: old.Data}).Write(record); e != nil {
		t.Fatal(e)
	}
	blocked := record
	blocked.ID, blocked.Generation = tasks.ID(), tasks.ID()
	blocked.State, blocked.Daemon, blocked.Provider = "active", before.AttemptID, "cloudflared"
	blocked.Auth, blocked.ProviderOwner, blocked.Identity = true, blocked.Generation, "previous-route"
	blocked.Pin.ID, blocked.Pin.Provider = blocked.ID, blocked.Provider
	if e := old.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		st.Network.Pins = append(st.Network.Pins, blocked.Pin)
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := (expose.Store{Data: old.Data}).Write(blocked); e != nil {
		t.Fatal(e)
	}
	if e := current.RequireCurrentDaemon(ctx); e == nil || e.Code != "old_daemon_version" {
		t.Fatal(e)
	}
	result, e := current.UpgradeNetwork(ctx, current.UpgradeRequestID(), hm)
	if e != nil {
		t.Fatal(e)
	}
	if rows := result.(map[string]any)["exposures"].([]map[string]any); len(rows) != 1 || rows[0]["reason"] != "provider_recovery_required" {
		t.Fatal(rows)
	}
	after, err := current.rpc(ctx, "status")
	if err != nil || after.Build != current.Build || after.AttemptID == before.AttemptID {
		t.Fatal(after, err)
	}
	status, e := current.NetworkStatus(ctx, "default")
	if e != nil || status["running"] != true {
		t.Fatal(status, e)
	}
	if rows, e := current.ExposureNavigation(ctx, false, nil); e != nil || len(rows) != 1 || rows[0].ID != blocked.ID {
		t.Fatal(rows, e)
	}
	if st, e := current.portStore().Read(); e != nil || !st.Pinned(route) {
		t.Fatal("provider ownership was released without proof", e)
	}
	provider.available.Store(true)
	result, e = current.UpgradeNetwork(ctx, current.UpgradeRequestID(), hm)
	if e != nil {
		t.Fatal(e)
	}
	if rows := result.(map[string]any)["exposures"].([]map[string]any); len(rows) != 1 || rows[0]["state"] != "auth_required" {
		t.Fatal(rows)
	}
	if live, err := current.rpc(ctx, "status"); err != nil || live.AttemptID != after.AttemptID {
		t.Fatal("provider retry replaced the restored daemon", live, err)
	}
	if st, e := current.portStore().Read(); e != nil || st.Pinned(route) {
		t.Fatal("settled exposures still own the route", e)
	}
}
