//go:build darwin

package ports

import (
	"context"
	"net"
	"strconv"
	"syscall"
)

func availabilityProbeAddresses(port int) ([]probeAddress, error) {
	targets := []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}}
	if port < 1024 {
		return targets, nil
	}
	local, err := localProbeAddresses()
	if err != nil {
		return nil, err
	}
	return append(targets, local...), nil
}

// Darwin permits unprivileged low-port wildcard binds, but address-specific
// low-port binds require privilege. Disable SO_REUSEADDR on the wildcard probe
// so an existing specific listener still makes the globally reserved port busy.
func listenAvailability(target probeAddress, port string) (net.Listener, error) {
	number, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	if number >= 1024 {
		return net.Listen(target.network, net.JoinHostPort(target.host, port))
	}
	return listenExclusiveAvailability(target, port)
}

func listenExclusiveAvailability(target probeAddress, port string) (net.Listener, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var optionErr error
		if err := raw.Control(func(fd uintptr) {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
		}); err != nil {
			return err
		}
		return optionErr
	}}
	return config.Listen(context.Background(), target.network, net.JoinHostPort(target.host, port))
}

// High-port probes retain Darwin's address-specific checks and reuse behavior.
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
