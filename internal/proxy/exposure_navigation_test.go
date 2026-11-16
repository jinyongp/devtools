package proxy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestExposureStopRecoversClosedAndAbandonedPins(t *testing.T) {
	ctx := context.Background()
	m := Manager{Data: t.TempDir()}
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	_, route := networkRoute(t, m, "app.localhost", "default", freeNetworkPort(t))
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	listener := st.Listener("default")
	backend := st.Get(route.InstanceID, "web").Port
	store := expose.Store{Data: m.Data}
	for _, state := range []string{"stopped", "starting"} {
		id, now := tasks.ID(), time.Now().UTC()
		pin := network.Pin{ID: id, Route: route, ListenerID: listener.ID, ListenerGeneration: listener.Generation, NetworkDigest: strings.Repeat("a", 64), Port: backend, Provider: "cloudflared", Service: "web"}
		record := expose.Record{Public: expose.Public{ID: id, Route: route, State: state, Generation: tasks.ID(), Provider: "cloudflared", Listener: "default", CreatedAt: now}, Pin: pin, Reservation: "origin." + strings.ReplaceAll(id, "-", ""), Identity: pin.NetworkDigest}
		if state == "stopped" {
			record.EndedAt = &now
		}
		record.OriginPort = freeNetworkPort(t)
		if e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			if _, e := st.Reference(record.Reservation, record.OriginPort); e != nil {
				return false, e
			}
			st.Network.Pins = append(st.Network.Pins, pin)
			return true, nil
		}); e != nil {
			t.Fatal(e)
		}
		if e := store.Write(record); e != nil {
			t.Fatal(e)
		}
		if state == "stopped" {
			current, e := m.ExposureNavigation(ctx, false, nil)
			if e != nil || len(current) != 1 || current[0].Reason != "release_pending" {
				t.Fatal(current, e)
			}
			if e := store.Retire(ctx, id); e == nil || e.Code != "exposure_recovery_required" {
				t.Fatal(e)
			}
		}
		stop := tasks.ID()
		if result, e := m.ExposeStop(ctx, id, stop); e != nil || result.(map[string]any)["changed"] != true {
			t.Fatal(result, e)
		}
		if result, e := m.ExposeStop(ctx, id, stop); e != nil || result.(map[string]any)["replayed"] != true {
			t.Fatal(result, e)
		}
		current, e := m.ExposureNavigation(ctx, false, nil)
		if e != nil || len(current) != 0 {
			t.Fatal(current, e)
		}
		st, e := m.portStore().Read()
		if e != nil || st.Pinned(route) || st.Reservation(record.Reservation) != nil {
			t.Fatal("stop did not release the exact registry resources", e)
		}
	}
}

func TestExposureNavigationCurrentAndHistoryPreserveUnresolved(t *testing.T) {
	data := filepath.Join(t.TempDir(), "private")
	if e := tasks.PrivateDir(data); e != nil {
		t.Fatal(e)
	}
	store := expose.Store{Data: data}
	states := []string{"stopped", "preparing", "auth_required", "recovery_required"}
	for _, state := range states {
		id := tasks.ID()
		route := network.RouteRef{Scope: "global", RouteID: tasks.ID()}
		pin := network.Pin{ID: id, Route: route, ListenerID: tasks.ID(), ListenerGeneration: tasks.ID(), NetworkDigest: strings.Repeat("a", 64), Port: 3000, Provider: "lan"}
		if e := store.Write(expose.Record{Public: expose.Public{ID: id, Route: route, State: state, Generation: tasks.ID(), Provider: "lan", Host: "public.example", CreatedAt: time.Now().UTC()}, Pin: pin}); e != nil {
			t.Fatal(e)
		}
	}
	m := Manager{Data: data}
	current, e := m.ExposureNavigation(context.Background(), false, nil)
	if e != nil || len(current) != 3 {
		t.Fatal(current, e)
	}
	for _, item := range current {
		if item.State == "stopped" {
			t.Fatal(item)
		}
	}
	history, e := m.ExposureNavigation(context.Background(), true, nil)
	if e != nil || len(history) != 4 {
		t.Fatal(history, e)
	}
	none, e := m.ExposureNavigation(context.Background(), true, func(expose.Public) bool { return false })
	if e != nil || len(none) != 0 {
		t.Fatal(none, e)
	}
}
