package ports

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// Available checks both supported address families without retaining listeners.
func Available(port int) (bool, *protocol.Error) {
	targets := []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}}
	if runtime.GOOS == "darwin" {
		// BSD permits a reusable wildcard listener alongside an address-specific
		// listener. Probe local addresses too, preserving SO_REUSEADDR so closed
		// connections in TIME_WAIT do not prevent a development server restart.
		local, err := localProbeAddresses()
		if err != nil {
			return false, storageError()
		}
		targets = append(targets, local...)
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

func localProbeAddresses() ([]probeAddress, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var targets []probeAddress
	seen := map[probeAddress]bool{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.IsUnspecified() || ip.IsMulticast() {
				continue
			}
			target := probeAddress{"tcp6", ip.String()}
			if ip.To4() != nil {
				target.network = "tcp4"
			} else if ip.IsLinkLocalUnicast() {
				target.host += "%" + iface.Name
			}
			if !seen[target] {
				seen[target] = true
				targets = append(targets, target)
			}
		}
	}
	return targets, nil
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
