package execution

import (
	"context"
	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"strings"
)

// PreparationFailure retains the domain condition and independent prerequisites
// when value authentication or dynamic binding preparation fails.
func PreparationFailure(ctx context.Context, c Command, parent []string, mode string, cause *protocol.Error) *protocol.Error {
	return preparationFailure(ctx, c, parent, mode, nil, cause)
}

func preparationFailure(ctx context.Context, c Command, parent []string, mode string, state *values.State, cause *protocol.Error) *protocol.Error {
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
	if c.Inject && state == nil {
		unknown = "environment_unresolved"
	}
	for key := range c.Bind {
		if strings.EqualFold(key, "PATH") || strings.EqualFold(key, "PATHEXT") {
			unknown = "binding_unresolved"
		}
	}
	exe := ""
	if len(c.Exec) > 0 {
		exe = c.Exec[0]
	}
	if strings.Contains(exe, "${") {
		unknown = "binding_unresolved"
	}
	checks := doctor.CheckRequirements(ctx, doctor.Input{Profile: c.Project.Profile, Command: c.Name, Directory: c.Directory, Env: c.Env, Requirements: c.Requirements, State: state, Inject: c.Inject, CheckInjection: true, Executable: exe, ExecutableOffset: c.Invocation.ExecutableOffset, Args: c.Arguments(), Environment: parent, EnvironmentUnknown: unknown, Mode: mode, ReadyExec: readyArguments(c), Source: commandSource(c)})
	for i := range checks {
		if checks[i].Status == "unknown" && (checks[i].Reason == "environment_unresolved" || checks[i].Reason == "binding_unresolved") {
			checks[i].BlockedBy = []string{"c1"}
		}
	}
	failure := doctor.FailureFor(checks, c.Diagnostic(mode))
	if failure == nil {
		failure = protocol.NewError("requirements_failed", "Cannot start command "+c.Name+".", 3, map[string]any{"phase": "preflight", "target": c.Diagnostic(mode).Target(), "checks": checks})
	}
	failure.Message = "Cannot start command " + c.Name + "."
	protocol.PreserveRecovery(failure, cause)
	if cause.Code == "canceled" || cause.Code == "deadline_exceeded" {
		failure.Code = cause.Code
		failure.ExitCode = cause.ExitCode
	}
	projected := protocol.Public(cause, "execution preparation")
	failure.Details["primary_cause_id"] = "c1"
	failure.Details["causes"] = append([]any{map[string]any{"id": "c1", "status": "fail", "message": cause.Message, "caused_by": []string{}, "remedies": projected.Details["remedies"], "code": cause.Code, "reason": projected.Details["reason"], "target": projected.Details["target"], "sources": projected.Details["sources"], "facts": projected.Details["facts"]}}, independentCauses(checks)...)
	failure.Details["remedies"] = projected.Details["remedies"]
	return failure
}

func independentCauses(checks []doctor.Check) []any {
	causes := []any{}
	for _, check := range checks {
		if check.Status != "fail" {
			continue
		}
		causes = append(causes, map[string]any{"code": check.Reason, "status": "fail", "message": check.Message, "caused_by": []string{}, "remedies": check.Remedies, "target": check.Target, "sources": check.Sources, "facts": check.Facts})
	}
	return causes
}
