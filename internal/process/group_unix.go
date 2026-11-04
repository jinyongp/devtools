//go:build !windows

package process

import (
	"context"
	"errors"
	"syscall"
	"time"
)

const stopGrace = 3 * time.Second

func signalOwnedGroup(group int, signal syscall.Signal) error {
	err := syscall.Kill(-group, signal)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	// Darwin's killpg skips zombies and can return EPERM for an existing
	// group with no live members. Only a fresh empty observation permits
	// completion; a denied signal to a live or unobservable group still fails.
	if errors.Is(err, syscall.EPERM) {
		alive, inspectErr := ownedGroupAlive(group)
		if inspectErr == nil && !alive {
			return nil
		}
	}
	return err
}

// The direct child is deliberately not waited/reaped here: its PID reserves the
// process-group identity through the final signal. Stream draining happens only
// after cleanup, so an inherited pipe cannot hide an early parent exit.
func waitOwnedGroup(ctx context.Context, pid int, tty *ttySession) error {
	ticker := time.NewTicker(10 * time.Millisecond)
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
