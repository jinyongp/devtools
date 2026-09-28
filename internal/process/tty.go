//go:build linux || darwin

package process

import (
	"errors"
	"fmt"
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
	shellPgrp  int
}

func ttyTrace(format string, args ...any) {
	path := os.Getenv("DEVTOOLS_TTY_TRACE")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, format+"\n", args...)
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
	shellPgrp := parent
	if ppid := os.Getppid(); ppid > 0 {
		if pgrp, err := syscall.Getpgid(ppid); err == nil && pgrp > 0 {
			shellPgrp = pgrp
		}
	}
	ttyTrace("detect pid=%d ppid=%d parent=%d shell=%d foreground=%d", os.Getpid(), os.Getppid(), parent, shellPgrp, foreground)
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
	ttyTrace("continue pid=%d parent=%d shell=%d child=%d observed=%d", os.Getpid(), t.parentPgrp, t.shellPgrp, childPgrp, foreground)
	if foreground == t.parentPgrp {
		if err := ttySetForegroundPgrp(t.fd, childPgrp); err != nil {
			return err
		}
		if next, nextErr := ttyForegroundPgrp(t.fd); nextErr == nil {
			ttyTrace("continue transfer child=%d foreground=%d", childPgrp, next)
		}
	}
	if err := syscall.Kill(-childPgrp, syscall.SIGCONT); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	ttyTrace("continue sigcont child=%d", childPgrp)
	return nil
}

func (t *ttySession) handleStop(childPgrp int) error {
	foreground, err := ttyForegroundPgrp(t.fd)
	if err != nil {
		return err
	}
	ttyTrace("stop pid=%d parent=%d shell=%d child=%d foreground=%d", os.Getpid(), t.parentPgrp, t.shellPgrp, childPgrp, foreground)
	if foreground == childPgrp {
		if err := ttySetForegroundPgrp(t.fd, t.parentPgrp); err != nil {
			return err
		}
		if next, nextErr := ttyForegroundPgrp(t.fd); nextErr == nil {
			ttyTrace("stop restore parent=%d foreground=%d", t.parentPgrp, next)
		}
	}
	// Stop the wrapper job group when this process is its leader so pipeline
	// peers stop with it. If the caller shares a non-job-control parent pgrp,
	// stop only this process to avoid suspending the parent shell.
	target := os.Getpid()
	if t.parentPgrp == target {
		target = -t.parentPgrp
	}
	ttyTrace("stop sigstop target=%d", target)
	if err := syscall.Kill(target, syscall.SIGSTOP); err != nil {
		return err
	}
	if next, nextErr := ttyForegroundPgrp(t.fd); nextErr == nil {
		ttyTrace("stop resumed pid=%d foreground=%d", os.Getpid(), next)
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
