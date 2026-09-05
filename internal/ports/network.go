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

// Available checks both supported address families without retaining listeners.
func Available(port int) (bool, *protocol.Error) {
	listeners := []net.Listener{}
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	for _, network := range []string{"tcp4", "tcp6"} {
		host := "0.0.0.0"
		if network == "tcp6" {
			host = "::"
		}
		l, e := net.Listen(network, net.JoinHostPort(host, strconv.Itoa(port)))
		if e != nil {
			if errors.Is(e, syscall.EAFNOSUPPORT) || errors.Is(e, syscall.EPROTONOSUPPORT) || network == "tcp6" && errors.Is(e, syscall.EADDRNOTAVAIL) {
				continue
			}
			if errors.Is(e, syscall.EADDRINUSE) {
				return false, nil
			}
			return false, storageError()
		}
		listeners = append(listeners, l)
	}
	if len(listeners) == 0 {
		return false, storageError()
	}
	return true, nil
}
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
func (st *State) Allocate(ctx context.Context, i Instance, name string, p project.Port, defaults []int) (Assignment, bool, *protocol.Error) {
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
	try := func(n int) (bool, *protocol.Error) {
		if ctx.Err() != nil {
			return false, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		if used[n] {
			return false, nil
		}
		return Available(n)
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
