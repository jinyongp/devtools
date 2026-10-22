package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

type PlanOptions struct {
	Mode      string
	Parent    process.ParentPolicy
	Selection string
}
type PlanSummary struct {
	Output    redact.Policy        `json:"output"`
	Profile   string               `json:"profile"`
	Command   string               `json:"command,omitempty"`
	Env       string               `json:"env,omitempty"`
	Inject    bool                 `json:"inject"`
	Selection string               `json:"selection"`
	Parent    process.ParentPolicy `json:"parent"`
}
type Plan struct {
	mode      string
	command   Command
	parent    []string
	summary   PlanSummary
	created   time.Time
	destroyed bool
}
type AuthenticatedSnapshot struct {
	plan      *Plan
	values    *values.Snapshot
	deadline  time.Time
	created   time.Time
	destroyed bool
}

func (Plan) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private execution plan cannot be serialized")
}
func (Plan) Format(s fmt.State, _ rune) { io.WriteString(s, "[REDACTED]") }
func (AuthenticatedSnapshot) MarshalJSON() ([]byte, error) {
	return nil, errors.New("authenticated snapshot cannot be serialized")
}
func (AuthenticatedSnapshot) Format(s fmt.State, _ rune) { io.WriteString(s, "[REDACTED]") }
func (p *Plan) Summary() PlanSummary {
	s := p.summary
	s.Parent.Allow = append([]string{}, s.Parent.Allow...)
	return s
}
func (p *Plan) Destroy() {
	if p != nil {
		clear(p.parent)
		p.parent = nil
		p.destroyed = true
	}
}
func ResolvePlan(command Command, parent []string, options PlanOptions) (*Plan, *protocol.Error) {
	if !command.Output.Valid() {
		return nil, protocol.NewError("invalid_argument", "Invalid output policy.", 2, nil)
	}
	if len(command.Exec) == 0 || command.Exec[0] == "" {
		return nil, protocol.NewError("invalid_argument", "Supply an executable.", 2, nil)
	}
	directory, err := filepath.Abs(command.Directory)
	if err != nil {
		return nil, protocol.NewError("io_error", "Cannot resolve execution directory.", 1, nil)
	}
	command.Directory = directory
	if options.Selection == "" {
		options.Selection = command.Selection
		if options.Selection == "" {
			options.Selection = "all"
		}
	}
	if !vault.ValidSelection(options.Selection) {
		return nil, protocol.NewError("invalid_argument", "Invalid value bundle selection.", 2, nil)
	}
	command.Selection = options.Selection
	if options.Mode == "" {
		options.Mode = "foreground"
	}
	if options.Mode != "foreground" && options.Mode != "managed" {
		return nil, protocol.NewError("invalid_argument", "Invalid execution preparation mode.", 2, nil)
	}
	selected, e := process.SelectParent(parent, options.Parent)
	if e != nil {
		return nil, e
	}
	if options.Parent.Mode == "" {
		options.Parent.Mode = process.ParentInherit
	}
	options.Parent.Allow = append([]string{}, options.Parent.Allow...)
	return &Plan{mode: options.Mode, command: cloneCommand(command), parent: process.CompleteEnvironment(selected), summary: PlanSummary{Output: command.Output.Normalize(), Profile: command.Project.Profile, Command: command.Name, Env: command.Env, Inject: command.Inject, Selection: options.Selection, Parent: options.Parent}, created: time.Now()}, nil
}
func loadSnapshot(ctx context.Context, command Command, dependencies Dependencies, includeSecrets bool) (*values.Snapshot, *protocol.Error) {
	if !command.NeedsValues() {
		return nil, nil
	}
	if dependencies.SelectedSnapshot != nil {
		return dependencies.SelectedSnapshot(ctx, command.Project.Profile, command.Env, includeSecrets, command.Selection)
	}
	if command.Selection != "" && command.Selection != "all" {
		return nil, protocol.NewError("value_selection_not_supported", "This snapshot provider does not support bundle selection.", 3, nil)
	}
	if dependencies.ValueSnapshot != nil {
		return dependencies.ValueSnapshot(ctx, command.Project.Profile, command.Env, includeSecrets)
	}
	state, e := (values.Store{Directory: dependencies.ValuesDirectory, Profile: command.Project.Profile}).Read(ctx)
	if e != nil {
		return nil, e
	}
	return values.CaptureLegacy(state, command.Env, includeSecrets)
}
func snapshotState(command Command, snapshot *values.Snapshot) (*values.State, *protocol.Error) {
	if snapshot == nil {
		if command.NeedsValues() {
			return nil, protocol.NewError("storage_error", "Value snapshot provider returned no snapshot.", 1, nil)
		}
		return nil, nil
	}
	want := command.Selection
	if want == "" {
		want = "all"
	}
	got := snapshot.Metadata().Selection
	if got == "" {
		got = "all"
	}
	if got != want {
		return nil, protocol.NewError("invalid_input_snapshot", "Value snapshot selection does not match the execution.", 3, nil)
	}
	state := snapshot.StateCopy()
	if state == nil || state.Profile != command.Project.Profile || (command.Inject && !snapshot.CanInject()) {
		return nil, protocol.NewError("invalid_input_snapshot", "Value snapshot does not match the selected execution.", 3, nil)
	}
	if e := state.CheckEnv(command.Env); e != nil {
		return nil, e
	}
	return state, nil
}
func readExecutionState(ctx context.Context, command Command, dependencies Dependencies) (*values.State, *protocol.Error) {
	snapshot, e := loadSnapshot(ctx, command, dependencies, command.Inject)
	if e != nil {
		return nil, e
	}
	defer snapshot.Destroy()
	return snapshotState(command, snapshot)
}
func AuthenticateSnapshot(ctx context.Context, plan *Plan, dependencies Dependencies) (*AuthenticatedSnapshot, *protocol.Error) {
	if plan == nil || plan.destroyed {
		return nil, snapshotError("invalid_input_snapshot")
	}
	if e := protocol.ContextError(ctx.Err()); e != nil {
		return nil, e
	}
	deadline := plan.created.Add(DefaultPreparationTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if !time.Now().Before(deadline) {
		return nil, snapshotError("input_snapshot_expired")
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	snapshot, e := loadSnapshot(ctx, plan.command, dependencies, plan.command.Inject)
	if e != nil {
		return nil, e
	}
	if _, e := snapshotState(plan.command, snapshot); e != nil {
		snapshot.Destroy()
		return nil, e
	}
	result := &AuthenticatedSnapshot{plan: plan, values: snapshot, deadline: deadline, created: plan.created}
	if e = result.validate(time.Now()); e != nil {
		result.Destroy()
		return nil, e
	}
	return result, nil
}
func (s *AuthenticatedSnapshot) validate(now time.Time) *protocol.Error {
	if s == nil || s.destroyed || s.plan == nil || s.plan.destroyed {
		return snapshotError("input_snapshot_unavailable")
	}
	if !now.Before(s.deadline) {
		return snapshotError("input_snapshot_expired")
	}
	if expiry := s.values.Metadata().EarliestExpiry; expiry != nil && !now.Before(*expiry) {
		return snapshotError("value_expired")
	}
	return nil
}
func (s *AuthenticatedSnapshot) Destroy() {
	if s != nil {
		s.values.Destroy()
		s.values = nil
		s.destroyed = true
	}
}

func Materialize(ctx context.Context, snapshot *AuthenticatedSnapshot, dependencies Dependencies) (Prepared, *protocol.Error) {
	if e := snapshot.validate(time.Now()); e != nil {
		return Prepared{}, e
	}
	if e := protocol.ContextError(ctx.Err()); e != nil {
		return Prepared{}, e
	}
	ctx, cancel := context.WithDeadline(ctx, snapshot.deadline)
	defer cancel()
	plan := snapshot.plan
	prepared, e := prepareResolved(ctx, cloneCommand(plan.command), snapshot.values.StateCopy(), dependencies, preparationOptions{injectValues: true, parentEnvironment: &plan.parent})
	if e != nil {
		return Prepared{}, PreparationFailure(ctx, plan.command, plan.parent, plan.mode, e)
	}
	prepared.mode = plan.mode
	if dependencies.IsolatedRoot != "" {
		prepared.Injected["DEVTOOLS_ISOLATED_ROOT"] = dependencies.IsolatedRoot
	}
	environment := process.CompleteEnvironment(process.Environment(plan.parent, prepared.Injected))
	if e = process.ValidateExecInput(prepared.Args, environment); e != nil {
		prepared.Release()
		return Prepared{}, e
	}
	prepared.probeEnvironment = probeEnvironment(plan.parent, prepared.State, plan.command.Env)
	pathBefore, _ := process.LookPath(prepared.Args[0], prepared.Command.Directory, environment)
	stampBefore, stampErr := process.ExecutableStamp(pathBefore)
	checks := prepared.Checks(ctx, prepared.probeEnvironment, true)
	if failure := doctor.Failure(checks, plan.command.Name, plan.command.Directory, plan.command.Project.Profile, plan.command.Env, plan.command.Selection, plan.mode, plan.command.Inject); failure != nil {
		prepared.Release()
		return Prepared{}, failure
	}
	prepared.checks = checks
	path, e := process.LookPath(prepared.Args[0], prepared.Command.Directory, environment)
	if e != nil {
		prepared.Release()
		return Prepared{}, e
	}
	if info, err := os.Stat(path); err != nil || !process.IsExecutable(info) {
		prepared.Release()
		return Prepared{}, protocol.NewError("executable_not_found", "The executable changed after prerequisite validation; diagnose the command again.", 127, map[string]any{"reason": "executable_changed", "checks": checks})
	}
	stamp, err := process.ExecutableStamp(path)
	if stampErr != nil || err != nil || stamp != stampBefore {
		prepared.Release()
		return Prepared{}, protocol.NewError("input_changed", "The executable changed or could not be verified during prerequisite checks. Prepare this command again.", 3, map[string]any{"phase": "preflight", "reason": "executable_changed", "checks": checks, "retry": "after_refresh"})
	}
	patterns := [][]byte{}
	if plan.command.Inject && prepared.State != nil {
		metadata, err := prepared.State.List(values.Secret, plan.command.Env)
		if err != nil {
			prepared.Release()
			return Prepared{}, err
		}
		original, err := prepared.State.Environment(plan.command.Env)
		if err != nil {
			prepared.Release()
			return Prepared{}, err
		}
		for _, item := range metadata {
			value := original[item.Key]
			if delivered, ok := process.EnvironmentValue(prepared.Injected, item.Key); ok && value == delivered && value != "" {
				patterns = append(patterns, []byte(value))
			}
		}
	}
	defer func() {
		for _, pattern := range patterns {
			clear(pattern)
		}
	}()
	inputID, err := vault.NewID()
	if err != nil {
		prepared.Release()
		return Prepared{}, snapshotError("input_snapshot_unavailable")
	}
	outputPlan, outputErr := redact.Resolve(plan.command.Output, dependencies.StdoutTTY, dependencies.StderrTTY, dependencies.CaptureOutput)
	if outputErr != nil {
		prepared.Release()
		return Prepared{}, protocol.NewError("invalid_argument", "Invalid output policy.", 2, nil)
	}
	for _, pattern := range patterns {
		if len(pattern) > 0 && len(pattern) < 4 {
			outputPlan.ShortSecrets = true
		}
	}
	metadata := InputMetadata{Output: outputPlan, InputID: inputID, Inject: plan.summary.Inject, Selection: plan.summary.Selection, Parent: plan.summary.Parent, Profile: plan.command.Project.Profile, Command: plan.command.Name, Env: plan.command.Env, PreparedAt: snapshot.created, PrepareDeadline: snapshot.deadline, Values: snapshot.values.Metadata()}
	spec := plan.command.ValueInputSpec()
	if dependencies.IsolatedRoot != "" {
		spec.Shadowed = append(spec.Shadowed, "DEVTOOLS_ISOLATED_ROOT")
	}
	metadata.Delivery, err = values.NewInputVersion(metadata.Values, plan.command.Env, spec)
	if err != nil {
		prepared.Release()
		return Prepared{}, values.VaultError(ctx, err)
	}
	prepared.Input, e = newInputPayload(inputWire{ExecutableStamp: stamp, VaultDirectory: dependencies.VaultDirectory, Served: prepared.Assignments, Format: 1, Metadata: metadata, Args: prepared.Args, Environment: environment, Directory: prepared.Command.Directory, Executable: path, Ready: cloneReady(prepared.Command.Ready), Patterns: patterns})
	if e != nil {
		prepared.Release()
		return Prepared{}, e
	}
	if e = snapshot.validate(time.Now()); e != nil {
		prepared.Input.Destroy()
		prepared.Release()
		return Prepared{}, e
	}
	release := prepared.Release
	payload := prepared.Input
	prepared.Release = func() { payload.Destroy(); release() }
	prepared.probeEnvironment = probeEnvironment(plan.parent, prepared.State, plan.command.Env)
	return prepared, nil
}
func PrepareInput(ctx context.Context, command Command, dependencies Dependencies, parent []string, options PlanOptions) (Prepared, *protocol.Error) {
	plan, e := ResolvePlan(command, parent, options)
	if e != nil {
		return Prepared{}, e
	}
	defer plan.Destroy()
	snapshot, e := AuthenticateSnapshot(ctx, plan, dependencies)
	if e != nil {
		return Prepared{}, PreparationFailure(ctx, plan.command, plan.parent, plan.mode, e)
	}
	defer snapshot.Destroy()
	return Materialize(ctx, snapshot, dependencies)
}

func probeEnvironment(parent []string, state *values.State, env string) []string {
	secrets := map[string]string{}
	if state != nil {
		if metadata, e := state.List(values.Secret, env); e == nil {
			for _, item := range metadata {
				secrets[item.Key] = ""
			}
		}
	}
	result := make([]string, 0, len(parent))
	for _, item := range parent {
		name, _, _ := strings.Cut(item, "=")
		if _, known := process.EnvironmentValue(secrets, name); !known {
			result = append(result, item)
		}
	}
	return result
}
