package services

import (
	"context"
	"os"

	"github.com/jinyongp/devtools/internal/process"
)

// The detached supervisor owns a console; each application has a separate Job.
func (s Store) serve(ctx context.Context, id string, inherited *os.File, execute Execute) error {
	if err := process.RequireDedicatedConsole(); err != nil {
		return err
	}
	return s.serveExecution(ctx, id, inherited, execute)
}
