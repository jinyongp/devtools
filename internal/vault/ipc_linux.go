package vault

import (
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func verifySessionPeer(c *net.UnixConn) error {
	sys, err := c.SyscallConn()
	if err != nil {
		return ErrDenied
	}
	var cred *unix.Ucred
	var failure error
	err = sys.Control(func(fd uintptr) { cred, failure = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil || failure != nil || cred == nil || cred.Uid != uint32(os.Getuid()) {
		return ErrDenied
	}
	return nil
}
