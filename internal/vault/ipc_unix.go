//go:build linux || darwin

package vault

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

type unixSessionListener struct{ listener *net.UnixListener }

func sessionEndpoint(directory, id string) string { return filepath.Join(directory, "session.sock") }
func listenSession(directory, id string) (sessionListener, error) {
	root, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	defer root.Close()
	path := sessionEndpoint(directory, id)
	if info, err := os.Lstat(path); err == nil {
		// The caller holds the private daemon lease before stale cleanup.
		if info.Mode()&os.ModeSocket == 0 {
			return nil, ErrRecovery
		}
		if err = os.Remove(path); err != nil {
			return nil, ErrRecovery
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRecovery
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, ErrUnavailable
	}
	if err = os.Chmod(path, 0600); err != nil {
		l.Close()
		return nil, ErrRecovery
	}
	return &unixSessionListener{l}, nil
}
func (l *unixSessionListener) Close() error { return l.listener.Close() }
func (l *unixSessionListener) Accept(ctx context.Context) (sessionConnection, error) {
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	for {
		c, err := l.listener.AcceptUnix()
		if err != nil {
			return nil, err
		}
		if err = verifySessionPeer(c); err != nil {
			c.Close()
			continue
		}
		return c, nil
	}
}
func dialSession(ctx context.Context, endpoint string) (sessionConnection, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, "unix", endpoint)
	if err != nil {
		return nil, ErrUnavailable
	}
	u := c.(*net.UnixConn)
	if err = verifySessionPeer(u); err != nil {
		c.Close()
		return nil, err
	}
	return u, nil
}
