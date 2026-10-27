package ports

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (st *State) UpgradeNetwork() {
	st.readIndex = nil
	if st.Version < 3 {
		st.Version = 3
	}
	if st.Reservations == nil {
		st.Reservations = []Reservation{}
	}
	if st.Network == nil {
		st.Network = &NetworkState{Listeners: []network.Listener{}, Routes: []network.GlobalRoute{}, Activations: []network.Activation{}, Pins: []network.Pin{}}
	}
}

// EnsureDefaultListener is used inside a candidate transaction. The final socket
// bind occurs later; existing backend numbers survive a bind permission failure.
func (st *State) EnsureDefaultListener(id, generation string) *protocol.Error {
	if st.Listener(network.DefaultListener) != nil {
		return nil
	}
	st.UpgradeNetwork()
	port := 20200
	if reservation := st.Reservation("proxy"); reservation != nil {
		port = reservation.Port
	}
	if _, e := st.Reference("proxy", port); e != nil {
		return e
	}
	st.Network.Listeners = append(st.Network.Listeners, network.Listener{ID: id, Name: network.DefaultListener, Generation: generation, HTTPReservation: "proxy", HTTPHost: "127.0.0.1", LegacyLoopback: true})
	return nil
}

func (st *State) Listener(name string) *network.Listener {
	if st.readIndex != nil {
		if n, ok := st.readIndex.listeners[network.ListenerName(name)]; ok {
			return &st.Network.Listeners[n]
		}
		return nil
	}
	if st.Network == nil {
		return nil
	}
	for i := range st.Network.Listeners {
		if st.Network.Listeners[i].Name == network.ListenerName(name) {
			return &st.Network.Listeners[i]
		}
	}
	return nil
}
func (st *State) GlobalRoute(name string) *network.GlobalRoute {
	if st.readIndex != nil {
		if n, ok := st.readIndex.routes[name]; ok {
			return &st.Network.Routes[n]
		}
		return nil
	}
	if st.Network == nil {
		return nil
	}
	for i := range st.Network.Routes {
		if st.Network.Routes[i].Name == name {
			return &st.Network.Routes[i]
		}
	}
	return nil
}
func (st *State) Activation(ref network.RouteRef) *network.Activation {
	if st.readIndex != nil {
		if n, ok := st.readIndex.activations[ref]; ok {
			return &st.Network.Activations[n]
		}
		return nil
	}
	if st.Network == nil {
		return nil
	}
	for i := range st.Network.Activations {
		if st.Network.Activations[i].Route == ref {
			return &st.Network.Activations[i]
		}
	}
	return nil
}
func (st *State) SetActivation(a network.Activation) *protocol.Error {
	if !a.Valid() {
		return fail("invalid_argument")
	}
	if st.Pinned(a.Route) {
		return fail("pinned")
	}
	st.UpgradeNetwork()
	if current := st.Activation(a.Route); current != nil {
		*current = a
	} else {
		st.Network.Activations = append(st.Network.Activations, a)
	}
	return nil
}
func (st *State) Pinned(ref network.RouteRef) bool {
	if st.readIndex != nil {
		return st.readIndex.pinned[ref]
	}
	if st.Network != nil {
		for _, p := range st.Network.Pins {
			if p.Route == ref {
				return true
			}
		}
	}
	return false
}
func (st *State) ListenerPinned(id string) bool {
	if st.readIndex != nil {
		return st.readIndex.listenerPins[id]
	}
	if st.Network != nil {
		for _, p := range st.Network.Pins {
			if p.ListenerID == id {
				return true
			}
		}
	}
	return false
}

func (st *State) ListenerPorts(name string) (int, int, *protocol.Error) {
	l := st.Listener(name)
	if l == nil {
		if network.ListenerName(name) != network.DefaultListener {
			return 0, 0, fail("listener_not_found")
		}
		if p := st.Reservation("proxy"); p != nil {
			return p.Port, 0, nil
		}
		return 20200, 0, nil
	}
	port := func(key string) int {
		if r := st.Reservation(key); r != nil {
			return r.Port
		}
		return 0
	}
	return port(l.HTTPReservation), port(l.HTTPSReservation), nil
}

// Reference reserves a fixed external backend without probing or owning its
// process. Other registry owners still make the number unavailable.
func (st *State) Reference(name string, port int) (bool, *protocol.Error) {
	st.readIndex = nil
	if !project.ValidProfile(name) || port < 1 || port > 65535 {
		return false, fail("invalid_argument")
	}
	for _, a := range st.Assignments {
		if a.Port == port {
			return false, fail("port_in_use")
		}
	}
	for _, r := range st.Reservations {
		if r.Port == port && r.Name != name {
			return false, fail("port_in_use")
		}
	}
	if r := st.Reservation(name); r != nil {
		if r.Port == port {
			return false, nil
		}
		r.Port = port
	} else {
		st.Reservations = append(st.Reservations, Reservation{Name: name, Port: port})
	}
	return true, nil
}

// ConfigureListener performs all reservation changes in one registry transaction.
// The caller owns lifecycle locking and checks that the listener is stopped.
func (s Store) ConfigureListener(ctx context.Context, name, httpAddress, httpsAddress string, disableHTTPS bool, identity ...string) (network.Listener, *protocol.Error) {
	name = network.ListenerName(name)
	if disableHTTPS && httpsAddress != "" {
		return network.Listener{}, protocol.NewError("invalid_argument", "Choose --https-address or --disable-https; they cannot be supplied together.", 2, nil)
	}
	if !project.ValidProfile(name) {
		return network.Listener{}, fail("invalid_argument")
	}
	var result network.Listener
	err := s.Update(ctx, func(st *State) (bool, *protocol.Error) {
		current := st.Listener(name)
		implicitHTTP := current == nil && name == network.DefaultListener && httpAddress == ""
		if current != nil && st.ListenerPinned(current.ID) {
			return false, fail("pinned")
		}
		st.UpgradeNetwork()
		if current == nil {
			l := network.Listener{ID: tasks.ID(), Name: name, Generation: tasks.ID()}
			l.LegacyLoopback = name == network.DefaultListener && httpAddress == ""
			if len(identity) == 2 {
				l.ID = identity[0]
			}
			if name == network.DefaultListener {
				if old := st.Reservation("proxy"); old != nil {
					l.HTTPHost = "127.0.0.1"
					l.HTTPReservation = "proxy"
				} else if httpAddress == "" {
					httpAddress = "127.0.0.1:20200"
				}
			}
			st.Network.Listeners = append(st.Network.Listeners, l)
			current = &st.Network.Listeners[len(st.Network.Listeners)-1]
		}
		set := func(address string, https bool) *protocol.Error {
			if address == "" {
				return nil
			}
			host, port, e := network.ParseAddress(address)
			if e != nil {
				return fail("invalid_argument")
			}
			key := "listener." + name + ".http"
			if name == network.DefaultListener && !https {
				key = "proxy"
			}
			if https {
				key = "listener." + name + ".https"
			}
			old := st.Reservation(key)
			if old == nil || old.Port != port {
				free, e := s.Available(port)
				if e != nil {
					return e
				}
				if !free {
					return fail("port_in_use")
				}
			}
			if _, e := st.Reference(key, port); e != nil {
				return e
			}
			if https {
				current.HTTPSHost = host
				current.HTTPSReservation = key
			} else {
				current.HTTPHost = host
				current.HTTPReservation = key
				current.LegacyLoopback = implicitHTTP
			}
			return nil
		}
		if e := set(httpAddress, false); e != nil {
			return false, e
		}
		if e := set(httpsAddress, true); e != nil {
			return false, e
		}
		if disableHTTPS && current.HTTPSReservation != "" {
			key := current.HTTPSReservation
			rs := st.Reservations[:0]
			for _, r := range st.Reservations {
				if r.Name != key {
					rs = append(rs, r)
				}
			}
			st.Reservations = rs
			current.HTTPSHost = ""
			current.HTTPSReservation = ""
		}
		current.Generation = tasks.ID()
		if len(identity) == 2 {
			if current.ID != identity[0] {
				return false, fail("request_conflict")
			}
			current.Generation = identity[1]
		}
		if !current.Valid() {
			return false, fail("invalid_argument")
		}
		result = *current
		return true, nil
	})
	return result, err
}

func (st *State) validNetwork() bool {
	if st.Network == nil {
		return true
	}
	n := st.Network
	if n.Listeners == nil || n.Routes == nil || n.Activations == nil || n.Pins == nil {
		return false
	}
	ids, names, owners := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, l := range n.Listeners {
		if !l.Valid() || ids[l.ID] || names[l.Name] {
			return false
		}
		ids[l.ID] = true
		names[l.Name] = true
		for _, r := range []string{l.HTTPReservation, l.HTTPSReservation} {
			if r != "" {
				if owners[r] || st.Reservation(r) == nil {
					return false
				}
				owners[r] = true
			}
		}
	}
	names = map[string]bool{}
	for _, r := range n.Routes {
		if !r.Valid() || ids[r.ID] || names[r.Name] || st.Reservation(r.Reservation) == nil || owners[r.Reservation] {
			return false
		}
		ids[r.ID] = true
		names[r.Name] = true
		owners[r.Reservation] = true
	}
	names = map[string]bool{}
	for _, a := range n.Activations {
		if !a.Valid() || names[a.Route.Key()] {
			return false
		}
		names[a.Route.Key()] = true
	}
	names = map[string]bool{}
	for _, p := range n.Pins {
		if !p.Valid() || names[p.ID] {
			return false
		}
		names[p.ID] = true
		l := st.ListenerForID(p.ListenerID)
		if l == nil || l.Generation != p.ListenerGeneration {
			return false
		}
		if p.Route.Scope == "project" {
			if a := st.Get(p.Route.InstanceID, p.Service); a == nil || a.Port != p.Port {
				return false
			}
		} else {
			found := false
			for _, r := range n.Routes {
				if r.ID == p.Route.RouteID {
					rr := st.Reservation(r.Reservation)
					found = rr != nil && rr.Port == p.Port
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func (st *State) ListenerForID(id string) *network.Listener {
	if st.readIndex != nil {
		if n, ok := st.readIndex.listenerIDs[id]; ok {
			return &st.Network.Listeners[n]
		}
		return nil
	}
	if st.Network != nil {
		for i := range st.Network.Listeners {
			if st.Network.Listeners[i].ID == id {
				return &st.Network.Listeners[i]
			}
		}
	}
	return nil
}
func (st *State) pinProjection(p network.Pin) string {
	var httpEndpoint, httpsEndpoint *Reservation
	if listener := st.ListenerForID(p.ListenerID); listener != nil {
		httpEndpoint = st.Reservation(listener.HTTPReservation)
		httpsEndpoint = st.Reservation(listener.HTTPSReservation)
	}
	var owner any
	if p.Route.Scope == "project" {
		owner = struct {
			Instance   *Instance
			Assignment *Assignment
		}{st.ByID(p.Route.InstanceID), st.Get(p.Route.InstanceID, p.Service)}
	} else if st.Network != nil {
		for _, r := range st.Network.Routes {
			if r.ID == p.Route.RouteID {
				owner = struct {
					Route       network.GlobalRoute
					Reservation *Reservation
				}{r, st.Reservation(r.Reservation)}
			}
		}
	}
	b, _ := json.Marshal(struct {
		Pin         network.Pin
		Owner       any
		Listener    *network.Listener
		Activation  *network.Activation
		HTTP, HTTPS *Reservation
	}{p, owner, st.ListenerForID(p.ListenerID), st.Activation(p.Route), httpEndpoint, httpsEndpoint})
	return string(b)
}

func (st *State) pinnedProjections() map[string]string {
	out := map[string]string{}
	if st.Network != nil {
		for _, p := range st.Network.Pins {
			out[p.ID] = st.pinProjection(p)
		}
	}
	return out
}

func (st *State) RemoveGlobal(name string) *protocol.Error {
	st.readIndex = nil
	r := st.GlobalRoute(name)
	if r == nil {
		return fail("route_not_found")
	}
	ref := r.Ref()
	if st.Pinned(ref) {
		return fail("pinned")
	}
	key := r.Reservation
	routes := st.Network.Routes[:0]
	for _, route := range st.Network.Routes {
		if route.ID != ref.RouteID {
			routes = append(routes, route)
		}
	}
	st.Network.Routes = routes
	activations := st.Network.Activations[:0]
	for _, a := range st.Network.Activations {
		if a.Route != ref {
			activations = append(activations, a)
		}
	}
	st.Network.Activations = activations
	reservations := st.Reservations[:0]
	for _, r := range st.Reservations {
		if r.Name != key {
			reservations = append(reservations, r)
		}
	}
	st.Reservations = reservations
	return nil
}

func GlobalReservation(id string) string { return "backend." + strings.ReplaceAll(id, "-", "") }

// AllocateUnion validates all selected peers against one temporary registry.
// It assigns numbers but takes no runtime claims and launches no processes.
// Returning an error from validate leaves the registry byte-for-byte unchanged.
func (s Store) AllocateUnion(ctx context.Context, p project.Context, commands []project.Command, defaults []int, validate func(*State) *protocol.Error, prepare ...func(*State) *protocol.Error) ([]Assignment, *protocol.Error) {
	var result []Assignment
	err := s.Update(ctx, func(st *State) (bool, *protocol.Error) {
		st.UpgradeNetwork()
		for _, before := range prepare {
			if e := before(st); e != nil {
				return false, e
			}
		}
		instance, e := st.Register(p.Profile, p.Root)
		if e != nil {
			return false, e
		}
		selected := map[string]bool{}
		for _, c := range commands {
			for _, name := range c.Serve {
				if _, ok := p.Ports[name]; !ok {
					return false, fail("invalid_config")
				}
				selected[name] = true
			}
		}
		names := make([]string, 0, len(selected))
		for name := range selected {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			a, _, e := s.Allocate(ctx, st, *instance, name, p.Ports[name], defaults)
			if e != nil {
				return false, e
			}
			result = append(result, a)
		}
		if validate != nil {
			if e := validate(st); e != nil {
				return false, e
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
