//go:build linux || darwin

package vault

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"time"
)

// The vault backend may unlink a stale socket because its daemon lease is
// exclusive. A runner in another language may not use our lease, so first
// refuse any live listener rather than unlinking its endpoint.
func peerEndpointVacant(ctx context.Context, address string) error {
	info, err := os.Lstat(address)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return ErrRecovery
	}
	probe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(probe, "unix", address)
	if err == nil {
		c.Close()
		return ErrUnavailable
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return ErrRecovery
}
