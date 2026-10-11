package ports

import (
	"context"

	"github.com/jinyongp/devtools/internal/protocol"
)

// PrepareGlobalRun claims the exact declared backend. Fixed route references
// may point at busy ports; a managed child must own a free port before exec.
func (s Store) PrepareGlobalRun(ctx context.Context, name string) (map[string]string, func(), *protocol.Error) {
	var env map[string]string
	release := func() {}
	e := s.Update(ctx, func(st *State) (bool, *protocol.Error) {
		route := st.GlobalRoute(name)
		if route == nil {
			return false, fail("route_not_found")
		}
		var pending []PendingBinding
		var e *protocol.Error
		env, pending, e = st.GlobalEnvironment(name)
		if e != nil {
			return false, e
		}
		if len(pending) > 0 {
			return false, fail(pending[0].Code)
		}
		reservation := st.Reservation(route.Reservation)
		if reservation == nil {
			return false, fail("port_not_found")
		}
		claimed, e := s.Claim(ctx, "global-"+route.ID, []string{"backend"})
		if e != nil {
			return false, e
		}
		release = claimed
		free, e := s.Available(reservation.Port)
		if e != nil {
			return false, e
		}
		if !free {
			return false, fail("port_in_use")
		}
		return false, nil
	})
	if e != nil {
		release()
		return nil, func() {}, e
	}
	return env, release, nil
}
