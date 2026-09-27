//go:build darwin

package process

import (
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

func ttyForegroundPgrp(fd int) (int, error) {
	return unix.IoctlGetInt(fd, unix.TIOCGPGRP)
}

func ttySetForegroundPgrp(fd, pgrp int) error {
	// Darwin does not expose pthread_sigmask through x/sys/unix. Ignore
	// SIGTTOU only around the foreground-group ioctl so a background wrapper
	// can restore or transfer terminal ownership without being stopped.
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgrp)
}
