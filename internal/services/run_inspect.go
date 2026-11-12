package services

import (
	"context"
	"sort"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type ServiceObservation struct {
	ports.RunObservation
	Command        string            `json:"command"`
	State          string            `json:"state"`
	Reason         string            `json:"reason"`
	Control        ControlStatus     `json:"control"`
	ExecutionID    string            `json:"execution_id,omitempty"`
	OwnerProfile   string            `json:"owner_profile,omitempty"`
	OwnerDirectory string            `json:"owner_directory,omitempty"`
	Remedies       []protocol.Remedy `json:"remedies"`
}

type ProjectInspection struct {
	Profile      string               `json:"profile"`
	Directory    string               `json:"directory"`
	Complete     bool                 `json:"complete"`
	Items        []Record             `json:"items"`
	Observations []ServiceObservation `json:"observations"`
	Diagnostics  []map[string]any     `json:"diagnostics"`
	NextActions  []protocol.Remedy    `json:"next_actions"`
}

// ExecutionDependencies attaches live recovery to the existing preparation
// pipeline. Port observers invoke it only after releasing their registry lock.
func (s Store) ExecutionDependencies(command execution.Command, configRoot string) (execution.Dependencies, *protocol.Error) {
	dependencies, failure := execution.DependenciesFor(command, s.Data, configRoot)
	if failure != nil {
		return dependencies, failure
	}
	dependencies.IsolatedRoot = s.IsolatedRoot
	dependencies.Ports.InspectFailure = func(ctx context.Context, failure *protocol.Error) *protocol.Error {
		return s.inspectRunFailure(ctx, command.Project, command.Name, failure)
	}
	return dependencies, nil
}

func (s Store) Ports() ports.Store {
	store := ports.ForDataRoot(s.Data)
	store.InspectFailure = func(ctx context.Context, failure *protocol.Error) *protocol.Error {
		target, _ := failure.Details["target"].(map[string]any)
		profile, _ := target["profile"].(string)
		directory, _ := target["directory"].(string)
		return s.inspectRunFailure(ctx, project.Context{Profile: profile, Root: directory}, "", failure)
	}
	return store
}

func stopRunAction(id string) protocol.Remedy {
	return (protocol.Action{Relation: protocol.SameStoreAction, Effect: "system_write", Message: "Stop the existing execution, then retry.", Intent: &protocol.CommandIntent{Command: "process stop", Arguments: []string{id}}}).Remedy(protocol.InvocationContext{})
}

func (s Store) resolveService(ctx context.Context, p project.Context, command string, observed ports.RunObservation) ServiceObservation {
	result := ServiceObservation{RunObservation: observed, Command: command, State: "available", Reason: "not_running", Control: ControlStatus{State: "unavailable", Reason: "not_running"}, Remedies: []protocol.Remedy{}}
	if observed.Claim == "active" {
		result.State, result.Reason = "unavailable", "owner_unverified"
		if reason, ok := observed.Owner["reason"].(string); ok && reason != "" {
			result.Reason = reason
		}
	} else if observed.Claim == "unavailable" {
		result.State, result.Reason = "unavailable", "claim_unavailable"
	} else if observed.PortObservation["status"] == "in_use" {
		result.State, result.Reason = "unmanaged", "no_verified_execution"
	} else if observed.PortObservation["status"] == "unavailable" && observed.PortObservation["reason"] != "assignment_missing" {
		result.State, result.Reason = "unavailable", "port_observation_unavailable"
		if reason, ok := observed.PortObservation["reason"].(string); ok && reason != "" {
			result.Reason = reason
		}
	}
	expected := ports.Instance{Profile: p.Profile, Directory: p.Root, ID: observed.InstanceID}
	owner := observed.Owner
	if observed.Claim == "inactive" && observed.Port != nil && observed.PortObservation["status"] == "in_use" {
		// A strict initial port can be held by another project. Match its exact
		// claim, never a program name or an arbitrary process using that port.
		if matched, identity, ok := s.claimForPort(ctx, *observed.Port); ok {
			owner, expected = matched, identity
			result.Owner = owner
		}
	}
	if owner["status"] != "verified" {
		result.Control.Reason = result.Reason
		return result
	}
	id, _ := owner["execution_id"].(string)
	if !validID(id) {
		result.State, result.Reason = "untracked", "execution_not_registered"
		result.Control.Reason = result.Reason
		return result
	}
	result.ExecutionID = id
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	finish := progress.Begin(ctx, "Checking execution control")
	defer finish()
	record, failure := s.Status(ctx, id)
	if failure != nil {
		result.State, result.Reason = "unavailable", failure.Code
		result.Control.Reason = failure.Code
		return result
	}
	ownerCommand, _ := owner["command"].(string)
	if record.Profile != expected.Profile || !location.Same(record.Directory, expected.Directory) || expected.ID != "" && record.Instance != expected.ID || ownerCommand != "" && record.Command != ownerCommand {
		result.State, result.Reason = "unavailable", "execution_scope_mismatch"
		result.Control.Reason = result.Reason
		return result
	}
	result.Control = record.Control
	result.OwnerProfile, result.OwnerDirectory = record.Profile, record.Directory
	if record.Control.Stop {
		result.State, result.Reason = "controlled", "authenticated_controller"
		result.Remedies = []protocol.Remedy{stopRunAction(id)}
	} else {
		result.State, result.Reason = "unavailable", record.Control.Reason
	}
	return result
}

func (s Store) claimForPort(ctx context.Context, port int) (map[string]any, ports.Instance, bool) {
	store := s.Ports()
	state, failure := store.Read()
	if failure != nil {
		return nil, ports.Instance{}, false
	}
	var owner map[string]any
	var identity ports.Instance
	for _, assignment := range state.Assignments {
		if ctx.Err() != nil {
			return nil, ports.Instance{}, false
		}
		if assignment.Port != port {
			continue
		}
		condition := store.Active(ctx, assignment.Instance.ID, assignment.Name)
		if condition == nil || condition.Code != "port_run_active" {
			continue
		}
		facts, _ := condition.Details["facts"].(map[string]any)
		candidate, _ := facts["owner"].(map[string]any)
		if candidate["status"] != "verified" {
			continue
		}
		if owner != nil {
			return nil, ports.Instance{}, false
		}
		owner, identity = candidate, assignment.Instance
	}
	return owner, identity, owner != nil
}

func (s Store) inspectRunFailure(ctx context.Context, p project.Context, command string, failure *protocol.Error) *protocol.Error {
	facts, _ := failure.Details["facts"].(map[string]any)
	if facts == nil || p.Profile == "" || p.Root == "" {
		return failure
	}
	observed := ports.RunObservation{Owner: map[string]any{}, Claim: "inactive"}
	observed.Service, _ = facts["service"].(string)
	observed.InstanceID, _ = facts["instance_id"].(string)
	observed.Owner, _ = facts["owner"].(map[string]any)
	observed.PortObservation, _ = facts["port_observation"].(map[string]any)
	if port, ok := facts["port"].(int); ok {
		observed.Port = &port
	}
	if failure.Code == "port_run_active" {
		observed.Claim = "active"
	}
	resolved := s.resolveService(ctx, p, command, observed)
	facts["owner"] = resolved.Owner
	if resolved.OwnerProfile != "" && (resolved.OwnerProfile != p.Profile || !location.Same(resolved.OwnerDirectory, p.Root)) {
		facts["owner_profile"], facts["owner_directory"] = resolved.OwnerProfile, resolved.OwnerDirectory
	}
	if resolved.Control.Stop {
		failure.Details["remedies"] = resolved.Remedies
	} else {
		facts["observation"] = map[string]any{"status": "unavailable", "reason": resolved.Reason, "missing": []string{"execution_control"}}
		action := ports.RunInspectionAction(resolved.Owner, p.Profile, p.Root, command, observed.Service)
		if resolved.State == "untracked" {
			action.Message = "This run is not registered for remote stop. Inspect its owner."
		}
		failure.Details["remedies"] = []protocol.Remedy{action.Remedy(protocol.InvocationContext{})}
	}
	return failure
}

// InspectProject keeps OS observations available even when older process records
// require migration. It does not fabricate records for untracked processes.
func (s Store) InspectProject(ctx context.Context, p project.Context, names []string) (ProjectInspection, *protocol.Error) {
	result := ProjectInspection{Profile: p.Profile, Directory: p.Root, Complete: true, Items: []Record{}, Observations: []ServiceObservation{}, Diagnostics: []map[string]any{}, NextActions: []protocol.Remedy{}}
	finish := progress.Begin(ctx, "Inspecting project services")
	defer finish()
	controlled := map[string]bool{}
	selected := map[string]bool{}
	all := len(names) == 0
	if all {
		for name := range p.Commands {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, protocol.ObservationDiagnostic(protocol.ContextError(err)))
			return result, nil
		}
		selected[name] = true
		definition, exists := p.Commands[name]
		if !exists {
			continue
		}
		observations, failure := s.Ports().ObserveCommand(ctx, p, definition)
		if failure != nil {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, protocol.ObservationDiagnostic(failure))
		}
		for _, observed := range observations {
			resolved := s.resolveService(ctx, p, name, observed)
			result.Observations = append(result.Observations, resolved)
			if resolved.Control.Stop && !controlled[resolved.ExecutionID] {
				result.NextActions = append(result.NextActions, resolved.Remedies...)
				controlled[resolved.ExecutionID] = true
			}
			if resolved.State == "unavailable" || observed.PortObservation["status"] == "in_use" && observed.Listener.Status != "observed" {
				result.Complete = false
			}
		}
	}
	records, failure := s.navigationRecordsContext(ctx)
	if failure != nil {
		result.Complete = false
		result.Diagnostics = append(result.Diagnostics, protocol.ObservationDiagnostic(failure))
		public := protocol.Public(failure, "project status")
		if remedies, ok := public.Details["remedies"].([]protocol.Remedy); ok {
			result.NextActions = append(result.NextActions, remedies...)
		}
		return result, nil
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, protocol.ObservationDiagnostic(protocol.ContextError(err)))
			return result, nil
		}
		if record.Profile != p.Profile || !location.Same(record.Directory, p.Root) || !all && !selected[record.Command] {
			continue
		}
		if record.EndedAt != nil && !unverifiedOwnedCleanup(record) {
			continue
		}
		current, failure := s.Status(ctx, record.ID)
		if failure != nil {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, protocol.ObservationDiagnostic(failure))
			continue
		}
		result.Items = append(result.Items, current)
		if current.Control.Stop && !controlled[current.ID] {
			result.NextActions = append(result.NextActions, stopRunAction(current.ID))
		}
	}
	return result, nil
}
