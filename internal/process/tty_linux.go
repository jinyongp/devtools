//go:build linux

package process

import (
	"runtime"
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
	if shellPgrp == parentPgrp || foreground != parentPgrp {
		return foreground, nil
	}

	// On Linux, bg can resume the wrapper while its pgrp is still foreground
	// briefly before the shell reclaims the terminal. Let that transition settle.
	deadline := time.Now().Add(100 * time.Millisecond)
	for foreground == parentPgrp && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		foreground, err = ttyForegroundPgrp(fd)
		if err != nil {
			return 0, err
		}
	}
	return foreground, nil
}

func ttySetForegroundPgrp(fd, pgrp int) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var set unix.Sigset_t
	signal := uint(syscall.SIGTTOU - 1)
	set.Val[signal/64] |= 1 << (signal % 64)
	var old unix.Sigset_t
	if err := unix.PthreadSigmask(unix.SIG_BLOCK, &set, &old); err != nil {
		return err
	}
	defer func() { _ = unix.PthreadSigmask(unix.SIG_SETMASK, &old, nil) }()
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgrp)
}
