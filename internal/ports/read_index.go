package ports

import "github.com/jinyongp/devtools/internal/network"

// registryReadIndex is rebuilt from a validated registry, and discarded before
// a transaction receives mutable slices. Positions avoid retaining slice pointers.
type registryReadIndex struct {
	instances    map[string]int
	directories  map[[2]string]int
	aliases      map[[2]string]int
	assignments  map[[2]string]int
	reservations map[string]int
	listeners    map[string]int
	listenerIDs  map[string]int
	routes       map[string]int
	activations  map[network.RouteRef]int
	pinned       map[network.RouteRef]bool
	listenerPins map[string]bool
}

func buildRegistryReadIndex(st *State) *registryReadIndex {
	r := &registryReadIndex{
		instances: map[string]int{}, directories: map[[2]string]int{}, aliases: map[[2]string]int{},
		assignments: map[[2]string]int{}, reservations: map[string]int{}, listeners: map[string]int{},
		listenerIDs: map[string]int{}, routes: map[string]int{}, activations: map[network.RouteRef]int{},
		pinned: map[network.RouteRef]bool{}, listenerPins: map[string]bool{},
	}
	for n, i := range st.Instances {
		r.instances[i.ID] = n
		r.directories[[2]string{i.Profile, i.Directory}] = n
		if i.Alias != nil {
			r.aliases[[2]string{i.Profile, *i.Alias}] = n
		}
	}
	for n, a := range st.Assignments {
		r.assignments[[2]string{a.ID, a.Name}] = n
	}
	for n, v := range st.Reservations {
		r.reservations[v.Name] = n
	}
	if st.Network != nil {
		for n, l := range st.Network.Listeners {
			r.listeners[l.Name], r.listenerIDs[l.ID] = n, n
		}
		for n, v := range st.Network.Routes {
			r.routes[v.Name] = n
		}
		for n, a := range st.Network.Activations {
			r.activations[a.Route] = n
		}
		for _, p := range st.Network.Pins {
			r.pinned[p.Route], r.listenerPins[p.ListenerID] = true, true
		}
	}
	return r
}
