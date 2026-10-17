package ports

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
	"net"
	"os"
	"syscall"
)

// SO_EXCLUSIVEADDRUSE is the bitwise complement of SO_REUSEADDR in Winsock.
// Set it before bind; plain Windows binds can otherwise share wildcard/specific
// addresses. Never enable reuse or change system reservations to force a bind.
const soExclusiveAddrUse = ^int32(syscall.SO_REUSEADDR)

func ListenTCP(ctx context.Context, network, address string) (net.Listener, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var optionErr error
		err := raw.Control(func(fd uintptr) {
			optionErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, int(soExclusiveAddrUse), 1)
		})
		if err != nil {
			return err
		}
		return optionErr
	}}
	return config.Listen(ctx, network, address)
}
func availabilityProbeAddresses(_ int) ([]probeAddress, error) {
	return []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp4", "127.0.0.1"}, {"tcp6", "::"}, {"tcp6", "::1"}}, nil
}
func listenAvailability(target probeAddress, port string) (net.Listener, error) {
	return ListenTCP(context.Background(), target.network, net.JoinHostPort(target.host, port))
}
func BindPermissionDenied(err error) bool {
	return errors.Is(err, windows.WSAEACCES) || errors.Is(err, os.ErrPermission)
}
func BindAddressInUse(err error) bool { return errors.Is(err, windows.WSAEADDRINUSE) }
func BindFamilyUnavailable(err error) bool {
	return errors.Is(err, windows.WSAEAFNOSUPPORT) || errors.Is(err, windows.WSAEPROTONOSUPPORT)
}
func availabilityAddressUnavailable(err error) bool {
	return BindFamilyUnavailable(err) || errors.Is(err, windows.WSAEADDRNOTAVAIL)
}
func availabilityError(err error) *protocol.Error {
	if BindPermissionDenied(err) {
		return protocol.NewError("permission_required", "Windows denied the TCP bind; the port may be reserved or blocked. Choose another port or check the system reservation. No network setting was changed.", 3, nil)
	}
	return storageError()
}
