//go:build linux || darwin

package process

import (
	"errors"
	"io"
	"os"
	"syscall"
)

type ttySession struct {
	fd         int
	parentPgrp int
	shellPgrp  int
}

func detectTTY(in io.Reader, out, diagnostic io.Writer) (*ttySession, bool) {
	input, ok := in.(*os.File)
	if !ok {
		return nil, false
	}
	fd := int(input.Fd())
	foreground, err := ttyForegroundPgrp(fd)
	if err != nil {
		return nil, false
	}
	parent := syscall.Getpgrp()
	if parent <= 0 || foreground != parent {
		return nil, false
	}
	shellPgrp := parent
	if ppid := os.Getppid(); ppid > 0 {
		if pgrp, err := syscall.Getpgid(ppid); err == nil && pgrp > 0 {
			shellPgrp = pgrp
		}
	}
	return &ttySession{fd: fd, parentPgrp: parent, shellPgrp: shellPgrp}, true
}

func (t *ttySession) childAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Foreground: true, Ctty: t.fd}
}

func (t *ttySession) restoreIfOwned(childPgrp int) error {
	foreground, err := ttyForegroundPgrp(t.fd)
	if err != nil {
		return err
	}
	if foreground != childPgrp {
		return nil
	}
	return ttySetForegroundPgrp(t.fd, t.parentPgrp)
}

func (t *ttySession) restoreAfterStartFailure() error {
	foreground, err := ttyForegroundPgrp(t.fd)
	if err != nil {
		return err
	}
	if foreground == t.parentPgrp {
		return nil
	}
	return ttySetForegroundPgrp(t.fd, t.parentPgrp)
}

func (t *ttySession) continueChild(childPgrp int) error {
	foreground, err := ttyResumeForegroundPgrp(t.fd, t.parentPgrp, t.shellPgrp)
	if err != nil {
		return err
	}
	if foreground == t.parentPgrp {
		if err := ttySetForegroundPgrp(t.fd, childPgrp); err != nil {
			return err
		}
	}
	if err := syscall.Kill(-childPgrp, syscall.SIGCONT); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (t *ttySession) handleStop(childPgrp int) error {
	foreground, err := ttyForegroundPgrp(t.fd)
	if err != nil {
		return err
	}
	if foreground == childPgrp {
		if err := ttySetForegroundPgrp(t.fd, t.parentPgrp); err != nil {
			return err
		}
	}
	// Stop the wrapper job group when this process is its leader so pipeline
	// peers stop with it. If the caller shares a non-job-control parent pgrp,
	// stop only this process to avoid suspending the parent shell.
	target := os.Getpid()
	if t.parentPgrp == target {
		target = -t.parentPgrp
	}
	if err := syscall.Kill(target, syscall.SIGSTOP); err != nil {
		return err
	}
	// Execution resumes here after the shell sends SIGCONT. If the wrapper was
	// foregrounded (fg), hand the terminal back to the child. If it was resumed
	// in the background (bg), continue the child without stealing the terminal.
	return t.continueChild(childPgrp)
}
