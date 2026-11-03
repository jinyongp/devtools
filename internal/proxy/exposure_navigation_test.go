package proxy

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/tasks"
)

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
