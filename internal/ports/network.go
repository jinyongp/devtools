package ports

import (
	"context"
	"errors"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// Available checks the platform's required TCP probe addresses without
// retaining listeners. Platform-specific target discovery lives behind build
// constraints so Linux availability checks never depend on interface netlink.
func Available(port int) (bool, *protocol.Error) {
	targets, err := availabilityProbeAddresses()
	if err != nil {
		return false, storageError()
	}
	checked := false
	for _, target := range targets {
		l, e := net.Listen(target.network, net.JoinHostPort(target.host, strconv.Itoa(port)))
		if e != nil {
			if errors.Is(e, syscall.EAFNOSUPPORT) || errors.Is(e, syscall.EPROTONOSUPPORT) || errors.Is(e, syscall.EADDRNOTAVAIL) {
				continue
			}
			if errors.Is(e, syscall.EADDRINUSE) {
				return false, nil
			}
			return false, storageError()
		}
		// Each probe closes before the next address is tested, so our own
		// wildcard listener cannot make a subsequent specific probe conflict.
		l.Close()
		checked = true
	}
	if !checked {
		return false, storageError()
	}
	return true, nil
}

type probeAddress struct{ network, host string }

func Reachable(ctx context.Context, port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		d := net.Dialer{Timeout: 200 * time.Millisecond}
		c, e := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if e == nil {
			c.Close()
			return true
		}
	}
	return false
}

func (s Store) Available(port int) (bool, *protocol.Error) {
	if s.Probe != nil {
		return s.Probe(port)
	}
	return Available(port)
}

func (s Store) Allocate(ctx context.Context, st *State, i Instance, name string, p project.Port, defaults []int) (Assignment, bool, *protocol.Error) {
	return st.allocate(ctx, i, name, p, defaults, s.Available)
}

func (st *State) Allocate(ctx context.Context, i Instance, name string, p project.Port, defaults []int) (Assignment, bool, *protocol.Error) {
	return st.allocate(ctx, i, name, p, defaults, Available)
}

func (st *State) allocate(ctx context.Context, i Instance, name string, p project.Port, defaults []int, probe AvailabilityProbe) (Assignment, bool, *protocol.Error) {
	if a := st.Get(i.ID, name); a != nil {
		return *a, false, nil
	}
	r := p.Range
	if r == nil {
		r = defaults
	}
	if !p.Valid() || !project.ValidRange(r) {
		return Assignment{}, false, fail("invalid_config")
	}
	used := map[int]bool{}
	for _, a := range st.Assignments {
		used[a.Port] = true
	}
	for _, reservation := range st.Reservations {
		used[reservation.Port] = true
	}
	try := func(n int) (bool, *protocol.Error) {
		if ctx.Err() != nil {
			return false, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		if used[n] {
			return false, nil
		}
		return probe(n)
	}
	selected := 0
	if p.Port != nil {
		ok, e := try(*p.Port)
		if e != nil {
			return Assignment{}, false, e
		}
		if ok {
			selected = *p.Port
		}
		if selected == 0 && p.Strict {
			return Assignment{}, false, fail("port_in_use")
		}
	}
	if selected == 0 {
		for n := r[0]; n <= r[1]; n++ {
			ok, e := try(n)
			if e != nil {
				return Assignment{}, false, e
			}
			if ok {
				selected = n
				break
			}
		}
	}
	if selected == 0 {
		return Assignment{}, false, fail("port_exhausted")
	}
	a := Assignment{Instance: i, Name: name, Port: selected}
	st.Assignments = append(st.Assignments, a)
	return a, true, nil
}

// Reserve keeps a user-global port unavailable to project assignments. A
// replacement is committed only after the new port is known to be available.
func (s Store) Reserve(ctx context.Context, name string, requested int) (int, bool, *protocol.Error) {
	if !project.ValidProfile(name) || requested < 1 || requested > 65535 {
		return 0, false, fail("invalid_config")
	}
	selected, changed := 0, false
	err := s.Update(ctx, func(state *State) (bool, *protocol.Error) {
		current := state.Reservation(name)
		if current != nil && current.Port == requested {
			selected = current.Port
			return false, nil
		}
		for _, assignment := range state.Assignments {
			if assignment.Port == requested {
				return false, fail("port_in_use")
			}
		}
		for _, reservation := range state.Reservations {
			if reservation.Name != name && reservation.Port == requested {
				return false, fail("port_in_use")
			}
		}
		free, err := s.Available(requested)
		if err != nil {
			return false, err
		}
		if !free {
			return false, fail("port_in_use")
		}
		if current == nil {
			state.Reservations = append(state.Reservations, Reservation{Name: name, Port: requested})
		} else {
			current.Port = requested
		}
		selected, changed = requested, true
		return true, nil
	})
	return selected, changed, err
}
