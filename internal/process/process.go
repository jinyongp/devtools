// Package process executes commands with explicit environment and stream inheritance.
package process

import (
	"context"

	"io"

	"syscall"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type SignalError struct{ Signal syscall.Signal }

// Runner lets a detached supervisor own the process lifetime while reusing
// command preparation, environment injection, and prerequisite checks.
type Runner func(context.Context, []string, string, []string, io.Reader, io.Writer, io.Writer) (int, *protocol.Error)
type runnerKey struct{}
type readyProbeKey struct{}

func WithRunner(ctx context.Context, runner Runner) context.Context {
	return context.WithValue(ctx, runnerKey{}, runner)
}

// WithReadyProbe carries the selected command readiness configuration alongside
// the prepared execution environment. Managed supervisors consume it only after
// the child has started successfully.
func WithReadyProbe(ctx context.Context, probe *project.ReadyProbe) context.Context {
	return context.WithValue(ctx, readyProbeKey{}, probe)
}

// ReadyProbe returns a defensive copy of the readiness configuration carried by
// the execution context.
func ReadyProbe(ctx context.Context) *project.ReadyProbe {
	probe, _ := ctx.Value(readyProbeKey{}).(*project.ReadyProbe)
	if probe == nil {
		return nil
	}
	cloned := *probe
	cloned.Exec = append([]string{}, probe.Exec...)
	return &cloned
}

func (s SignalError) Error() string { return "execution interrupted by signal" }
