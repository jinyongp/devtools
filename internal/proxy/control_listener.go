package proxy

import (
	"context"
	"errors"
	"net"

	"github.com/jinyongp/devtools/internal/ports"
)

// A configured frontend can still be unbound when the daemon starts. Do not
// let its ephemeral control socket claim a port already promised by the registry.
func (m Manager) listenControl(ctx context.Context) (net.Listener, error) {
	listen := m.ControlListen
	if listen == nil {
		listen = func() (net.Listener, error) { return ports.ListenTCP(ctx, "tcp4", "127.0.0.1:0") }
	}
	for range 16 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		socket, err := listen()
		if err != nil {
			return nil, err
		}
		address, ok := socket.Addr().(*net.TCPAddr)
		if !ok {
			_ = socket.Close()
			return nil, errors.New("invalid control listener address")
		}
		// Read after binding, so any reservations committed during allocation
		// are also respected. Later availability probes see this bound socket.
		state, failure := m.portStore().Read()
		if failure != nil {
			_ = socket.Close()
			return nil, failure
		}
		claimed := false
		for _, assignment := range state.Assignments {
			claimed = claimed || assignment.Port == address.Port
		}
		for _, reservation := range state.Reservations {
			claimed = claimed || reservation.Port == address.Port
		}
		if !claimed {
			return socket, nil
		}
		_ = socket.Close()
	}
	return nil, errors.New("cannot allocate an unreserved control port")
}
