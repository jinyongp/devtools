// Package process executes commands with explicit environment and stream inheritance.
package process

import (
	"context"

	"io"
	"os"

	"path/filepath"
	"sort"
	"strings"
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

func Environment(parent []string, injected map[string]string) []string {
	merged := map[string]string{}
	for _, item := range parent {
		if key, value, ok := strings.Cut(item, "="); ok {
			merged[key] = value
		}
	}
	for key, value := range injected {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+merged[key])
	}
	return result
}

// LookPath resolves an executable using the command's directory and environment.
func LookPath(name, dir string, env []string) (string, *protocol.Error) {
	check := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
	}
	if strings.ContainsRune(name, filepath.Separator) {
		if !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		// Start reports missing paths and permission failures without printing args.
		return name, nil
	}
	search := ""
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			search = strings.TrimPrefix(item, "PATH=")
		}
	}
	for _, part := range filepath.SplitList(search) {
		if !filepath.IsAbs(part) {
			part = filepath.Join(dir, part)
		}
		candidate := filepath.Join(part, name)
		if check(candidate) {
			return candidate, nil
		}
	}
	return "", protocol.NewError("command_not_found", "Executable was not found in the selected PATH.", 127, nil)
}
