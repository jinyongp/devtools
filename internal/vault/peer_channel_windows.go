package vault

import (
	"context"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func listenPeerEndpoint(directory, id, _ string) (PeerListener, error) {
	// FILE_FLAG_FIRST_PIPE_INSTANCE in the shared backend refuses squatting.
	return listenSession(directory, id)
}

func dialPeerEndpoint(ctx context.Context, directory, address string) (PeerConnection, error) {
	// Named pipes have no filesystem parent. Preserve their existing private
	// storage guard and the shared backend's SID/pipe checks.
	guard, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	defer guard.Close()
	return dialSession(ctx, address)
}
