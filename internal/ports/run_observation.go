package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

// describeRunFailure uses the registry already held by the caller. It never
// reacquires storage, reserves a port, queries a provider or stops a process.
func describeRunFailure(e *protocol.Error, profile, directory, instance, service string, port int) *protocol.Error {
	if e == nil || e.Code != "port_run_active" && e.Code != "port_in_use" {
		return e
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	facts, _ := e.Details["facts"].(map[string]any)
	if facts == nil {
		facts = map[string]any{}
	}
	facts["service"], facts["instance_id"] = service, instance
	if port > 0 {
		facts["port"] = port
		facts["port_observation"] = map[string]any{"status": "not_checked", "tcp_reachable": nil, "reason": nil}
	} else {
		facts["port"] = nil
		facts["port_observation"] = map[string]any{"status": "unavailable", "tcp_reachable": nil, "reason": "assignment_missing"}
	}
	owner, _ := facts["owner"].(map[string]any)
	if owner == nil {
		owner = unavailableOwner("unsupported_owner_query")
		facts["owner"] = owner
	}
	e.Details["facts"] = facts
	e.Details["target"] = map[string]any{"kind": "service", "name": service, "profile": profile, "directory": directory}
	e.Details["phase"] = "prepare"
	if e.Code == "port_run_active" {
		e.Message = fmt.Sprintf("Service %q is claimed by another run.", service)
	} else {
		e.Message = "The TCP port for service " + service + " is already in use."
	}
	command, _ := owner["command"].(string)
	action := RunInspectionAction(owner, profile, directory, command, service)
	e.Details["remedies"] = []protocol.Remedy{action.Remedy(protocol.InvocationContext{})}
	return e
}

// RunInspectionAction never treats persisted ownership as live control authority.
// The services layer upgrades it to stop only after an authenticated observation.
func RunInspectionAction(owner map[string]any, profile, directory, command, service string) protocol.Action {
	if command == "" {
		command, _ = owner["command"].(string)
	}
	if command == "" && owner["status"] == "verified" && (owner["kind"] == "managed" || owner["kind"] == "foreground") {
		if id, ok := owner["execution_id"].(string); ok && id != "" {
			return protocol.Action{Relation: protocol.SameStoreAction, Effect: "read", Message: "Inspect the existing execution's control state.", Intent: &protocol.CommandIntent{Command: "process status", Arguments: []string{id}}}
		}
	}
	intent := &protocol.CommandIntent{Command: "project status", Options: []protocol.ActionOption{{Name: "profile", Value: profile}, {Name: "dir", Value: directory}}}
	if command != "" {
		intent.Arguments = []string{command}
	}
	message := "Inspect the service and its owner."
	if profile == "" || directory == "" {
		intent = &protocol.CommandIntent{Command: "proxy env", Arguments: []string{service}, Options: []protocol.ActionOption{{Name: "global", Value: "true"}}}
		message = "Inspect the global backend."
	}
	return protocol.Action{Relation: protocol.SameStoreAction, Effect: "read", Message: message, Intent: intent}
}

func (s Store) finishRunFailure(ctx context.Context, failure *protocol.Error) *protocol.Error {
	failure = observeRunFailure(ctx, failure)
	if s.InspectFailure != nil && failure != nil && (failure.Code == "port_run_active" || failure.Code == "port_in_use") {
		return s.InspectFailure(ctx, failure)
	}
	return failure
}

// Observation happens after the registry lock is released, within the caller's
// remaining deadline and a one-second ceiling. It only connects to loopback.
func observeRunFailure(ctx context.Context, e *protocol.Error) *protocol.Error {
	if e == nil || e.Code != "port_run_active" && e.Code != "port_in_use" {
		return e
	}
	facts, _ := e.Details["facts"].(map[string]any)
	port, ok := facts["port"].(int)
	if !ok || port < 1 || port > 65535 {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	status, reason := "free", any(nil)
	var reachable any = false
	for _, host := range []string{"127.0.0.1", "::1"} {
		connection, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			connection.Close()
			status, reachable, reason = "in_use", true, nil
			break
		}
		if ctx.Err() != nil {
			status, reachable, reason = "unavailable", nil, "deadline_exceeded"
			break
		}
		if !connectionRefused(err) {
			status, reachable, reason = "unavailable", nil, "tcp_probe_failed"
		}
	}
	if e.Code == "port_in_use" && status == "free" {
		// A failed bind is independent of accepting loopback TCP connections.
		status = "in_use"
	}
	facts["port_observation"] = map[string]any{"status": status, "tcp_reachable": reachable, "reason": reason}
	if status == "in_use" {
		facts["listener"] = ObserveListener(ctx, port)
	}
	return e
}

func connectionRefused(err error) bool {
	var operation *net.OpError
	return errors.As(err, &operation) && refusedConnection(operation.Err)
}
