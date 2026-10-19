package services

import (
	"context"
	"sort"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/values"
)

type ValueUse struct {
	Kind           string                  `json:"kind"`
	Command        string                  `json:"command"`
	Directory      string                  `json:"directory"`
	Env            string                  `json:"env"`
	Selection      string                  `json:"selection"`
	ExecutionID    string                  `json:"execution_id,omitempty"`
	ExecutionState string                  `json:"execution_state,omitempty"`
	Policy         *redact.Policy          `json:"output_policy,omitempty"`
	Output         *redact.Plan            `json:"output,omitempty"`
	Values         values.InputObservation `json:"values"`
}

func (s Store) InspectCommandValues(ctx context.Context, command execution.Command) (*values.InputVersion, *protocol.Error) {
	spec := command.ValueInputSpec()
	if s.IsolatedRoot != "" {
		spec.Shadowed = append(spec.Shadowed, "DEVTOOLS_ISOLATED_ROOT")
	}
	return values.InspectInput(ctx, s.Data, command.Project.Profile, command.Env, spec)
}

// InspectRecordValues follows the same current configuration and explicit
// overrides as a manual restart. It never prepares a launcher or unlocks keys.
func (s Store) InspectRecordValues(ctx context.Context, r Record) values.InputObservation {
	if r.StartedAt == nil || r.State == "unknown" {
		return values.InputObservation{State: "unknown", Reason: "delivery_not_confirmed"}
	}
	if r.InputVersion == nil {
		return values.InputObservation{State: "unknown", Reason: "delivery_unknown"}
	}
	p, e := project.Resolve(r.Directory, "")
	if e != nil {
		return values.ObserveInput(r.InputVersion, nil, e)
	}
	if p.Profile != r.Profile || p.Root != r.Directory {
		return values.ObserveInput(r.InputVersion, nil, protocol.NewError("instance_conflict", "Project identity changed.", 3, nil))
	}
	command, e := execution.ResolveConfigured(p, r.Command, r.EnvOverride, nil)
	if e != nil {
		return values.ObserveInput(r.InputVersion, nil, e)
	}
	if r.BundleOverride != nil {
		command.Selection = *r.BundleOverride
	}
	s.IsolatedRoot = r.IsolatedRoot
	current, e := s.InspectCommandValues(ctx, command)
	return values.ObserveInput(r.InputVersion, current, e)
}

// ValueUses separates configuration declarations from retained delivery records.
// Only records from this canonical project directory are included.
func (s Store) ValueUses(ctx context.Context, p project.Context) ([]ValueUse, *protocol.Error) {
	items := []ValueUse{}
	names := make([]string, 0, len(p.Commands))
	for name := range p.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		command, e := execution.ResolveConfigured(p, name, nil, nil)
		if e != nil {
			return nil, e
		}
		current, e := s.InspectCommandValues(ctx, command)
		policy := command.Output.Normalize()
		observation := values.ObserveInput(nil, current, e)
		if e == nil {
			observation.Reason = "declared_only"
		}
		items = append(items, ValueUse{Kind: "declared", Command: name, Directory: p.Root, Env: command.Env, Selection: command.Selection, Policy: &policy, Values: observation})
	}
	records, e := s.List(ctx, p.Profile)
	if e != nil {
		return nil, e
	}
	for _, r := range records {
		if r.Directory != p.Root {
			continue
		}
		selection := r.Selection
		if selection == "" {
			selection = "all"
		}
		kind := "delivered"
		if r.StartedAt == nil {
			kind = "prepared"
		}
		items = append(items, ValueUse{Kind: kind, Command: r.Command, Directory: r.Directory, Env: r.Env, Selection: selection, ExecutionID: r.ID, ExecutionState: r.State, Output: r.Output, Values: s.InspectRecordValues(ctx, r)})
	}
	return items, nil
}
