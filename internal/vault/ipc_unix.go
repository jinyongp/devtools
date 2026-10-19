//go:build linux || darwin

package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/jinyongp/devtools/internal/platformfs"
)

type unixSessionListener struct {
	listener           *net.UnixListener
	temporaryDirectory string
	closeOnce          sync.Once
	closeError         error
}

func sessionEndpoint(directory, id string) string {
	path := filepath.Join(directory, "session.sock")
	// Both supported Unix platforms have a small sockaddr_un path ceiling.
	// Keep normal roots in place; long isolated/user paths get their own private
	// short directory, still bound to this data root by endpoint metadata and peers.
	if len(path) < 100 {
		return path
	}
	sum := sha256.Sum256([]byte(filepath.Clean(directory)))
	temporary := "/tmp"
	if canonical, err := filepath.EvalSymlinks(temporary); err == nil {
		temporary = canonical
	}
	name := "devtools-vault-" + strconv.Itoa(os.Getuid()) + "-" + hex.EncodeToString(sum[:16])
	return filepath.Join(temporary, name, "session.sock")
}
func listenSession(directory, id string) (sessionListener, error) {
	root, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	defer root.Close()
	path := sessionEndpoint(directory, id)
	temporaryDirectory := ""
	if filepath.Dir(path) != directory {
		guard, err := platformfs.OpenRoot(filepath.Dir(path), true)
		if err != nil {
			return nil, ErrRecovery
		}
		guard.Close()
		temporaryDirectory = filepath.Dir(path)
	}
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
	return &unixSessionListener{listener: l, temporaryDirectory: temporaryDirectory}, nil
}
func (l *unixSessionListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeError = l.listener.Close()
		if l.temporaryDirectory != "" {
			_ = os.Remove(l.temporaryDirectory)
		}
	})
	return l.closeError
}
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
