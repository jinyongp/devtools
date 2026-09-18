//go:build !darwin

package ports

func availabilityProbeAddresses() ([]probeAddress, error) {
	return []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}}, nil
}
