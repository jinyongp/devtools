//go:build linux || darwin

package vault

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func privatePeerPath(path string, mode os.FileMode) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrRecovery
	}
	if !ownedPeerPath(info) || info.Mode()&(os.ModeType|os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != mode {
		return nil, ErrRecovery
	}
	return info, nil
}

func ownedPeerPath(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
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

type unixPeerListener struct {
	*unixSessionListener
	guard     *platformfs.Root
	address   string
	parent    os.FileInfo
	socket    os.FileInfo
	temporary bool
	once      sync.Once
	err       error
}

func (l *unixPeerListener) Close() error {
	l.once.Do(func() {
		l.err = l.unixSessionListener.Close()
		parent, parentErr := os.Lstat(filepath.Dir(l.address))
		socket, socketErr := os.Lstat(l.address)
		// Closing an old listener must leave a successor's endpoint alone.
		if parentErr == nil && socketErr == nil && ownedPeerPath(parent) && ownedPeerPath(socket) && os.SameFile(l.parent, parent) && os.SameFile(l.socket, socket) {
			l.err = errors.Join(l.err, os.Remove(l.address))
			if l.temporary {
				_ = os.Remove(filepath.Dir(l.address))
			}
		}
		l.err = errors.Join(l.err, l.guard.Close())
	})
	return l.err
}

func listenPeerEndpoint(directory, _ string, address string) (PeerListener, error) {
	parentPath := filepath.Dir(address)
	guard, err := platformfs.OpenRoot(parentPath, true)
	if err != nil {
		return nil, ErrRecovery
	}
	parent, err := privatePeerPath(parentPath, os.ModeDir|0700)
	if err != nil {
		guard.Close()
		return nil, err
	}
	if _, err := os.Lstat(address); err == nil {
		guard.Close()
		if _, err := privatePeerPath(address, os.ModeSocket|0600); err != nil {
			return nil, err
		}
		return nil, ErrUnavailable
	} else if !errors.Is(err, os.ErrNotExist) {
		guard.Close()
		return nil, ErrRecovery
	}
	// External receivers do not share our internal lease. Bind without unlinking
	// any existing socket, including an apparently stale one: a probe followed by
	// unlink could remove a receiver that bound between those two operations.
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		guard.Close()
		return nil, ErrUnavailable
	}
	l.SetUnlinkOnClose(false)
	socket, err := os.Lstat(address)
	if err != nil || socket.Mode()&os.ModeSocket == 0 || !ownedPeerPath(socket) {
		l.Close()
		guard.Close()
		return nil, ErrRecovery
	}
	listener := &unixPeerListener{unixSessionListener: &unixSessionListener{listener: l}, guard: guard, address: address, parent: parent, socket: socket, temporary: parentPath != directory}
	if err := os.Chmod(address, 0600); err != nil {
		listener.Close()
		return nil, ErrRecovery
	}
	nextParent, parentErr := privatePeerPath(parentPath, os.ModeDir|0700)
	nextSocket, socketErr := privatePeerPath(address, os.ModeSocket|0600)
	if parentErr != nil || socketErr != nil || !os.SameFile(parent, nextParent) || !os.SameFile(socket, nextSocket) {
		listener.Close()
		return nil, ErrRecovery
	}
	return listener, nil
}
