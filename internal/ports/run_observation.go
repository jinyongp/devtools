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
	action := protocol.Action{Relation: protocol.ManualAction, Effect: "read", Message: "Stop the existing run with Ctrl+C in its original terminal or through its controller, then retry this command."}
	if owner["status"] == "verified" && owner["kind"] == "managed" {
		if id, ok := owner["execution_id"].(string); ok && id != "" {
			action = protocol.Action{Relation: protocol.SameStoreAction, Effect: "system_write", Message: "Stop the existing managed execution, then retry this command.", Intent: &protocol.CommandIntent{Command: "process stop", Arguments: []string{id}, NewRequest: true}}
		}
	}
	e.Details["remedies"] = []protocol.Remedy{action.Remedy(protocol.InvocationContext{})}
	return e
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
	return e
}

func connectionRefused(err error) bool {
	var operation *net.OpError
	return errors.As(err, &operation) && refusedConnection(operation.Err)
}
