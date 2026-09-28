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

func ttyWaitStatusContinued(status syscall.WaitStatus) bool {
	return status.Continued() || (status.Stopped() && status.StopSignal() == syscall.SIGCONT)
}

func ttyResumeForegroundPgrp(fd, parentPgrp, _ int) (int, error) {
	foreground, err := ttyForegroundPgrp(fd)
	if err != nil {
		return 0, err
	}
	if foreground == parentPgrp {
		return foreground, nil
	}

	// Darwin bash can deliver SIGCONT while some other pgrp still owns the
	// terminal, then foreground the wrapper moments later for fg. The wrapper's
	// immediate parent is not a reliable proxy for the interactive shell pgrp,
	// so detect fg only by observing our own pgrp become foreground. If that
	// never happens within the handoff window, this is a background resume.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		foreground, err = ttyForegroundPgrp(fd)
		if err != nil {
			return 0, err
		}
		if foreground == parentPgrp {
			return foreground, nil
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
