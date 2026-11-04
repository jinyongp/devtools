//go:build !windows

package process

import (
	"context"
	"errors"
	"syscall"
	"time"
)

const stopGrace = 3 * time.Second

// The direct child is deliberately not waited/reaped here: its PID reserves the
// process-group identity through the final signal. Stream draining happens only
// after cleanup, so an inherited pipe cannot hide an early parent exit.
func waitOwnedGroup(ctx context.Context, pid int, tty *ttySession) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var deadline time.Time
	forced := false
	canceled := ctx.Done()
	for {
		state, err := ownedChildState(pid)
		if err != nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return err
		}
		if deadline.IsZero() && (ctx.Err() != nil || state == childExited) {
			signal := syscall.SIGTERM
			var cause SignalError
			if errors.As(context.Cause(ctx), &cause) {
				signal = cause.Signal
			}
			if err := syscall.Kill(-pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
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
				return nil
			}
			if time.Now().After(deadline) {
				if forced {
					return errors.New("owned process group did not exit")
				}
				if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
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
