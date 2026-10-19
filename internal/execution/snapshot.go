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

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

type PlanOptions struct {
	Parent    process.ParentPolicy
	Selection string
}
type PlanSummary struct {
	Profile   string               `json:"profile"`
	Command   string               `json:"command,omitempty"`
	Env       string               `json:"env,omitempty"`
	Inject    bool                 `json:"inject"`
	Selection string               `json:"selection"`
	Parent    process.ParentPolicy `json:"parent"`
}
type Plan struct {
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
	if len(command.Exec) == 0 || command.Exec[0] == "" {
		return nil, protocol.NewError("invalid_argument", "Supply an executable.", 2, nil)
	}
	directory, err := filepath.Abs(command.Directory)
	if err != nil {
		return nil, protocol.NewError("io_error", "Cannot resolve execution directory.", 1, nil)
	}
	command.Directory = directory
	if options.Selection == "" {
		options.Selection = "all"
	}
	if options.Selection != "all" {
		return nil, protocol.NewError("value_selection_not_supported", "This execution format currently selects the full profile and env.", 2, nil)
	}
	selected, e := process.SelectParent(parent, options.Parent)
	if e != nil {
		return nil, e
	}
	if options.Parent.Mode == "" {
		options.Parent.Mode = process.ParentInherit
	}
	options.Parent.Allow = append([]string{}, options.Parent.Allow...)
	return &Plan{command: cloneCommand(command), parent: process.CompleteEnvironment(selected), summary: PlanSummary{Profile: command.Project.Profile, Command: command.Name, Env: command.Env, Inject: command.Inject, Selection: options.Selection, Parent: options.Parent}, created: time.Now()}, nil
}
func loadSnapshot(ctx context.Context, command Command, dependencies Dependencies, includeSecrets bool) (*values.Snapshot, *protocol.Error) {
	if !command.NeedsValues() {
		return nil, nil
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
	if ctx.Err() != nil {
		return nil, protocol.NewError("canceled", "Execution preparation canceled.", 130, nil)
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
	if ctx.Err() != nil {
		return Prepared{}, protocol.NewError("canceled", "Execution preparation canceled.", 130, nil)
	}
	ctx, cancel := context.WithDeadline(ctx, snapshot.deadline)
	defer cancel()
	plan := snapshot.plan
	prepared, e := prepareResolved(ctx, cloneCommand(plan.command), snapshot.values.StateCopy(), dependencies, preparationOptions{injectValues: true, parentEnvironment: &plan.parent})
	if e != nil {
		return Prepared{}, e
	}
	environment := process.CompleteEnvironment(process.Environment(plan.parent, prepared.Injected))
	if e = process.ValidateExecInput(prepared.Args, environment); e != nil {
		prepared.Release()
		return Prepared{}, e
	}
	path, e := process.LookPath(prepared.Args[0], prepared.Command.Directory, environment)
	if e != nil {
		prepared.Release()
		return Prepared{}, e
	}
	if info, err := os.Stat(path); err != nil || !process.IsExecutable(info) {
		prepared.Release()
		return Prepared{}, protocol.NewError("command_not_found", "Prepared executable is unavailable.", 127, nil)
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
	metadata := InputMetadata{Inject: plan.summary.Inject, Selection: plan.summary.Selection, Parent: plan.summary.Parent, Profile: plan.command.Project.Profile, Command: plan.command.Name, Env: plan.command.Env, PreparedAt: snapshot.created, PrepareDeadline: snapshot.deadline, Values: snapshot.values.Metadata()}
	prepared.Input, e = newInputPayload(inputWire{Format: 1, Metadata: metadata, Args: prepared.Args, Environment: environment, Directory: prepared.Command.Directory, Executable: path, Ready: cloneReady(prepared.Command.Ready), Patterns: patterns})
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
		return Prepared{}, e
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
