package execution

import (
	"context"
	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/protocol"
	"strings"
)

// PreparationFailure retains the domain condition and independent prerequisites
// when value authentication or dynamic binding preparation fails.
func PreparationFailure(ctx context.Context, c Command, parent []string, mode string, cause *protocol.Error) *protocol.Error {
	if cause == nil {
		return nil
	}
	switch cause.Code {
	case "invalid_input_snapshot", "input_snapshot_expired", "input_snapshot_unavailable", "input_snapshot_consumed", "value_selection_not_supported":
		return cause
	}
	if cause.Code == "invalid_argument" || cause.Code == "invalid_config" || cause.Code == "command_not_found" && cause.ExitCode == 3 {
		return cause
	}
	unknown := ""
	if c.Inject {
		unknown = "environment_unresolved"
	}
	exe := ""
	if len(c.Exec) > 0 {
		exe = c.Exec[0]
	}
	if strings.Contains(exe, "${") {
		unknown = "binding_unresolved"
	}
	checks := doctor.CheckRequirements(ctx, doctor.Input{Profile: c.Project.Profile, Command: c.Name, Directory: c.Directory, Env: c.Env, Requirements: c.Requirements, Inject: c.Inject, CheckInjection: true, Executable: exe, Args: c.Arguments(), Environment: parent, EnvironmentUnknown: unknown, Mode: mode, ReadyExec: readyArguments(c), Source: commandSource(c)})
	checks = append(checks, doctor.NewCheck("preparation", "fail", cause.Code, cause.Message))
	failure := doctor.FailureFor(checks, c.Diagnostic(mode))
	if cause.Code == "canceled" || cause.Code == "deadline_exceeded" {
		failure.Code = cause.Code
		failure.ExitCode = cause.ExitCode
	}
	projected := protocol.Public(cause, "execution preparation")
	failure.Details["causes"] = []any{map[string]any{"code": cause.Code, "reason": projected.Details["reason"], "target": projected.Details["target"], "sources": projected.Details["sources"], "facts": projected.Details["facts"]}}
	return failure
}
