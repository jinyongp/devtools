//go:build !windows

package services

import (
	"context"
	"errors"
	"os"
	"syscall"
)

// Detached launchers must keep their session independent of the invoking CLI.
// Actual application process ownership is shared with foreground execution.
func (s Store) serve(ctx context.Context, id string, inherited *os.File, execute Execute) error {
	pid := os.Getpid()
	group, err := syscall.Getpgid(pid)
	if err != nil || group != pid {
		return errors.New("dedicated session required")
	}
	return s.serveExecution(ctx, id, inherited, execute)
}
