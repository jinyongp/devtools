//go:build !windows

package process

import (
	"context"
	"errors"
	"runtime"
	"syscall"
	"time"
)

const stopGrace = 3 * time.Second
const ownedGroupPoll = 10 * time.Millisecond

func signalOwnedGroup(group int, signal syscall.Signal) error {
	deadline := time.Now().Add(stopGrace)
	for {
		err := syscall.Kill(-group, signal)
		if err == nil || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if !errors.Is(err, syscall.EPERM) {
			return err
		}
		// Darwin can stop accepting group signals before the exiting member
		// appears as a zombie. Reconcile that gap while the leader still owns
		// the group ID. A persistent denial or failed inspection remains an error.
		alive, inspectErr := ownedGroupAlive(group)
		if inspectErr != nil {
			return err
		}
		if !alive {
			return nil
		}
		if runtime.GOOS != "darwin" || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(ownedGroupPoll)
	}
}

// The direct child is deliberately not waited/reaped here: its PID reserves the
// process-group identity through the final signal. Stream draining happens only
// after cleanup, so an inherited pipe cannot hide an early parent exit.
func waitOwnedGroup(ctx context.Context, pid int, tty *ttySession) error {
	ticker := time.NewTicker(ownedGroupPoll)
	defer ticker.Stop()
	var deadline time.Time
	forced := false
	quiet := false
	verifiedKill := false
	canceled := ctx.Done()
	for {
		state, err := ownedChildState(pid)
		if err != nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return err
		}
		if deadline.IsZero() && (ctx.Err() != nil || state == childExited) {
			notifyCleanupStarted(ctx)
			signal := syscall.SIGTERM
			var cause SignalError
			if errors.As(context.Cause(ctx), &cause) {
				signal = cause.Signal
			}
			if err := signalOwnedGroup(pid, signal); err != nil {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				return err
			}
			if signal != syscall.SIGKILL {
				_ = syscall.Kill(-pid, syscall.SIGCONT)
			}
			deadline = time.Now().Add(stopGrace)
			canceled = nil
		}
		if !deadline.IsZero() {
			alive, err := ownedGroupAlive(pid)
			if err != nil {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				return err
			}
			if !alive {
				if verifiedKill {
					return nil
				}
				if quiet {
					// A process may fork between the membership snapshot and
					// its own exit. Confirm a quiet sweep, close that race with
					// a kernel group signal, then observe emptiness once more.
					if err := signalOwnedGroup(pid, syscall.SIGKILL); err != nil {
						return err
					}
					verifiedKill = true
				}
				quiet = true
			} else {
				quiet = false
			}
			if time.Now().After(deadline) {
				if forced {
					return errors.New("owned process group did not exit")
				}
				if err := signalOwnedGroup(pid, syscall.SIGKILL); err != nil {
					return err
				}
				forced = true
				deadline = time.Now().Add(stopGrace)
			}
		} else if state == childStopped && tty != nil {
			if err := tty.handleStop(pid); err != nil {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				return err
			}
		}
		select {
		case <-ticker.C:
		case <-canceled:
		}
	}
}

type childState uint8

const (
	childRunning childState = iota
	childStopped
	childExited
)
