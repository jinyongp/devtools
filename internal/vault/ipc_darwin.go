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
	var cred *unix.Xucred
	var failure error
	err = sys.Control(func(fd uintptr) { cred, failure = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED) })
	if err != nil || failure != nil || cred == nil || cred.Uid != uint32(os.Getuid()) {
		return ErrDenied
	}
	return nil
}
