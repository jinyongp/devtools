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

func ttyResumeForegroundPgrp(fd, parentPgrp, _ int) (int, error) {
	foreground, err := ttyForegroundPgrp(fd)
	if err != nil {
		return 0, err
	}
	// Darwin bash foregrounds the wrapper before delivering SIGCONT for fg.
	// Treat that observation immediately as foreground resume; waiting lets the
	// shell reclaim the terminal before the wrapper can hand it to the child.
	return foreground, nil
}

func ttySetForegroundPgrp(fd, pgrp int) error {
	// Darwin does not expose pthread_sigmask through x/sys/unix. Ignore
	// SIGTTOU only around the foreground-group ioctl so a background wrapper
	// can restore or transfer terminal ownership without being stopped.
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgrp)
}
