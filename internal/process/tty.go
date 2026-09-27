//go:build linux || darwin

package process

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

type ttySession struct {
	fd         int
	parentPgrp int
}

func directWriter(writer io.Writer) bool {
	if writer == nil {
		return true
	}
	_, ok := writer.(*os.File)
	return ok
}

func detectTTY(in io.Reader, out, diagnostic io.Writer) (*ttySession, bool) {
	input, ok := in.(*os.File)
	if !ok || !directWriter(out) || !directWriter(diagnostic) {
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
	return &ttySession{fd: fd, parentPgrp: parent}, true
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
	foreground, err := ttyForegroundPgrp(t.fd)
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
	if err := syscall.Kill(target, syscall.SIGTSTP); err != nil {
		return err
	}
	// Execution resumes here after the shell sends SIGCONT. If the wrapper was
	// foregrounded (fg), hand the terminal back to the child. If it was resumed
	// in the background (bg), continue the child without stealing the terminal.
	return t.continueChild(childPgrp)
}

func waitInteractive(ctxDone <-chan struct{}, cause func() error, command *exec.Cmd, tty *ttySession) (int, *protocol.Error) {
	pid := command.Process.Pid
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-done:
			return
		case <-ctxDone:
		}
		signal := syscall.SIGTERM
		var signalCause SignalError
		if errors.As(cause(), &signalCause) {
			signal = signalCause.Signal
		}
		_ = syscall.Kill(-pid, signal)
		// A stopped child cannot act on TERM/INT/HUP until continued.
		if signal != syscall.SIGKILL {
			_ = syscall.Kill(-pid, syscall.SIGCONT)
		}
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		case <-timer.C:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()

	var exitCode int
	var waitErr error
	for {
		var status syscall.WaitStatus
		_, err := syscall.Wait4(pid, &status, syscall.WUNTRACED|syscall.WCONTINUED, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			waitErr = err
			break
		}
		switch {
		case status.Exited():
			exitCode = status.ExitStatus()
			waitErr = nil
			goto finished
		case status.Signaled():
			exitCode = 128 + int(status.Signal())
			waitErr = nil
			goto finished
		case status.Stopped():
			if ctxDone != nil {
				select {
				case <-ctxDone:
					_ = syscall.Kill(-pid, syscall.SIGCONT)
					continue
				default:
				}
			}
			if err := tty.handleStop(pid); err != nil {
				waitErr = err
				goto finished
			}
		case status.Continued():
			continue
		}
	}

finished:
	_ = tty.restoreIfOwned(pid)
	close(done)
	<-watcherDone
	_ = command.Process.Release()
	if waitErr != nil {
		return 0, protocol.NewError("io_error", "Command stream handling failed.", 1, nil)
	}
	return exitCode, nil
}
