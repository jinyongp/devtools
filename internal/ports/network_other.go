//go:build !darwin && !windows

package ports

import "net"

func availabilityProbeAddresses(_ int) ([]probeAddress, error) {
	return []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}}, nil
}

func listenAvailability(target probeAddress, port string) (net.Listener, error) {
	return net.Listen(target.network, net.JoinHostPort(target.host, port))
}
