//go:build linux || darwin

package vault

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func privatePeerPath(path string, mode os.FileMode) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrRecovery
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode()&(os.ModeType|os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != mode {
		return nil, ErrRecovery
	}
	return info, nil
}

func dialPeerEndpoint(ctx context.Context, _ string, address string) (PeerConnection, error) {
	// The returned endpoint is the complete transport contract. Its private
	// parent may be outside the data root when Unix socket paths are shortened.
	directory := filepath.Dir(address)
	guard, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	defer guard.Close()
	parent, err := privatePeerPath(directory, os.ModeDir|0700)
	if err != nil {
		return nil, err
	}
	socket, err := privatePeerPath(address, os.ModeSocket|0600)
	if err != nil {
		return nil, err
	}
	c, err := dialSession(ctx, address)
	if err != nil {
		return nil, err
	}
	// Check named objects again before any protocol bytes can be sent. Never
	// accept a replaced socket or parent merely because the peer has our UID.
	nextParent, parentErr := privatePeerPath(directory, os.ModeDir|0700)
	nextSocket, socketErr := privatePeerPath(address, os.ModeSocket|0600)
	if parentErr != nil || socketErr != nil || !os.SameFile(parent, nextParent) || !os.SameFile(socket, nextSocket) {
		c.Close()
		return nil, ErrRecovery
	}
	return c, nil
}

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
