//go:build !windows

package ports

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/protocol"
	"net"
	"syscall"
)

// ListenTCP uses the platform's normal listener policy on Unix.
func ListenTCP(ctx context.Context, network, address string) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(ctx, network, address)
}
func BindPermissionDenied(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}
func BindAddressInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
func BindFamilyUnavailable(err error) bool {
	return errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EPROTONOSUPPORT)
}
func availabilityAddressUnavailable(err error) bool {
	return BindFamilyUnavailable(err) || errors.Is(err, syscall.EADDRNOTAVAIL)
}
func availabilityError(error) *protocol.Error { return storageError() }
