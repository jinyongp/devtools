//go:build darwin

package ports

import "net"

func availabilityProbeAddresses() ([]probeAddress, error) {
	targets := []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}}
	local, err := localProbeAddresses()
	if err != nil {
		return nil, err
	}
	return append(targets, local...), nil
}

// localProbeAddresses covers Darwin's BSD socket semantics, where the reusable
// wildcard probes used by net.Listen can coexist with an address-specific
// listener. Enumerating interfaces is intentionally Darwin-only.
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
