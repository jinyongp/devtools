package vault

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

// These aliases expose only authenticated local transport, never key operations.
type PeerConnection = sessionConnection
type PeerListener = sessionListener

var peerUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func peerDirectory(root, runner, instance string) (string, error) {
	if !filepath.IsAbs(root) || !peerUUID.MatchString(runner) || !peerUUID.MatchString(instance) {
		return "", ErrInvalid
	}
	return filepath.Join(root, "value-runner-transports", runner, instance), nil
}
func PeerAddress(root, runner, instance string) (string, error) {
	dir, err := peerDirectory(root, runner, instance)
	if err != nil {
		return "", err
	}
	return sessionEndpoint(dir, strings.ReplaceAll(instance, "-", "")), nil
}

type ownedPeerListener struct {
	PeerListener
	release func()
	guard   *platformfs.Root
	once    sync.Once
	err     error
}

func (l *ownedPeerListener) Close() error {
	l.once.Do(func() { l.err = l.PeerListener.Close(); l.release(); l.guard.Close() })
	return l.err
}
func ListenPeer(ctx context.Context, root, runner, instance string) (PeerListener, error) {
	dir, err := peerDirectory(root, runner, instance)
	if err != nil {
		return nil, err
	}
	guard, err := platformfs.OpenRoot(dir, true)
	if err != nil {
		return nil, ErrRecovery
	}
	release, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(dir, "listener.lock"))
	if err != nil || !free {
		guard.Close()
		return nil, ErrUnavailable
	}
	address, _ := PeerAddress(root, runner, instance)
	if err = peerEndpointVacant(ctx, address); err != nil {
		release()
		guard.Close()
		return nil, err
	}
	listener, err := listenSession(dir, strings.ReplaceAll(instance, "-", ""))
	if err != nil {
		release()
		guard.Close()
		return nil, err
	}
	return &ownedPeerListener{PeerListener: listener, release: release, guard: guard}, nil
}
func DialPeer(ctx context.Context, root, runner, instance string) (PeerConnection, error) {
	dir, err := peerDirectory(root, runner, instance)
	if err != nil {
		return nil, err
	}
	address, _ := PeerAddress(root, runner, instance)
	return dialPeerEndpoint(ctx, dir, address)
}
