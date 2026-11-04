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
type executableKey struct{}
type cleanupStartedKey struct{}

// WithCleanupStarted observes owned-scope termination without changing child
// streams or confusing a stop request with verified completion.
func WithCleanupStarted(ctx context.Context, started func()) context.Context {
	return context.WithValue(ctx, cleanupStartedKey{}, started)
}

func notifyCleanupStarted(ctx context.Context) {
	if started, ok := ctx.Value(cleanupStartedKey{}).(func()); ok {
		started()
	}
}

func cleanupFailure(scope string) *protocol.Error {
	return protocol.NewError("process_cleanup_incomplete", "Cannot verify cleanup of the owned process scope.", 3, map[string]any{"effects": "unknown", "facts": map[string]any{"cleanup_state": "unknown", "ownership_scope": scope}})
}

// WithExecutablePath binds a previously resolved executable to an actual input
// snapshot. argv[0] retains the caller's original spelling.
func WithExecutablePath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, executableKey{}, path)
}
func executablePath(ctx context.Context) string {
	path, _ := ctx.Value(executableKey{}).(string)
	return path
}

func WithRunner(ctx context.Context, runner Runner) context.Context {
	return context.WithValue(ctx, runnerKey{}, runner)
}

// HasRunner identifies a caller-owned execution boundary. Such a runner must
// remain authoritative; a CLI controller cannot claim its OS process ownership.
func HasRunner(ctx context.Context) bool {
	_, ok := ctx.Value(runnerKey{}).(Runner)
	return ok
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
