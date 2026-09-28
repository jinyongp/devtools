//go:build darwin

package process

import (
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func ttyForegroundPgrp(fd int) (int, error) {
	return unix.IoctlGetInt(fd, unix.TIOCGPGRP)
}

func ttyResumeForegroundPgrp(fd, parentPgrp, shellPgrp int) (int, error) {
	foreground, err := ttyForegroundPgrp(fd)
	if err != nil {
		return 0, err
	}
	if shellPgrp == parentPgrp || foreground != shellPgrp {
		return foreground, nil
	}

	// Darwin bash can deliver SIGCONT while the shell still owns the terminal,
	// then foreground the wrapper moments later for fg. Wait only until that
	// wrapper handoff appears and return immediately; continuing to wait lets
	// bash reclaim the terminal and makes fg indistinguishable from bg.
	deadline := time.Now().Add(100 * time.Millisecond)
	for foreground == shellPgrp && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		foreground, err = ttyForegroundPgrp(fd)
		if err != nil {
			return 0, err
		}
		if foreground == parentPgrp {
			break
		}
	}
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
