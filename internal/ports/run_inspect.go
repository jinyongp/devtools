package ports

import (
	"context"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type RunObservation struct {
	Service         string              `json:"service"`
	InstanceID      string              `json:"instance_id"`
	Port            *int                `json:"port"`
	Claim           string              `json:"claim"`
	Owner           map[string]any      `json:"owner"`
	PortObservation map[string]any      `json:"port_observation"`
	Listener        ListenerObservation `json:"listener"`
}

// ObserveCommand reads existing assignments and probes their current claim and
// TCP state. It never allocates a port, releases a claim or opens profile values.
func (s Store) ObserveCommand(ctx context.Context, p project.Context, command project.Command) ([]RunObservation, *protocol.Error) {
	if err := ctx.Err(); err != nil {
		return nil, protocol.ContextError(err)
	}
	state, failure := s.Read()
	if failure != nil {
		return nil, failure
	}
	instance := state.Find(p.Profile, "", p.Root)
	result := make([]RunObservation, 0, len(command.Serve))
	for _, service := range command.Serve {
		if err := ctx.Err(); err != nil {
			return result, protocol.ContextError(err)
		}
		observation := RunObservation{Service: service, Claim: "inactive", Owner: unavailableOwner("unsupported_owner_query"), PortObservation: map[string]any{"status": "unavailable", "tcp_reachable": nil, "reason": "assignment_missing"}, Listener: ListenerObservation{Status: "unavailable", Reason: "not_checked", Processes: []ListenerProcess{}}}
		if instance != nil {
			observation.InstanceID = instance.ID
			if assignment := state.Get(instance.ID, service); assignment != nil {
				port := assignment.Port
				observation.Port = &port
			}
		}
		if observation.Port == nil {
			if definition, ok := p.Ports[service]; ok && definition.Strict && definition.Port != nil {
				port := *definition.Port
				observation.Port = &port
			}
		}
		var condition *protocol.Error
		if instance != nil {
			condition = s.Active(ctx, instance.ID, service)
		}
		if condition != nil && condition.Code == "port_run_active" {
			observation.Claim = "active"
			facts, _ := condition.Details["facts"].(map[string]any)
			if owner, ok := facts["owner"].(map[string]any); ok {
				observation.Owner = owner
			}
		} else if condition != nil {
			observation.Claim = "unavailable"
			observation.Owner = unavailableOwner("metadata_unavailable")
		}
		if observation.Port != nil {
			port := *observation.Port
			free, unavailable := s.Available(port)
			if unavailable != nil {
				observation.PortObservation = map[string]any{"status": "unavailable", "tcp_reachable": nil, "reason": unavailable.Code}
			} else {
				code := "port_run_active"
				if !free {
					code = "port_in_use"
				}
				e := protocol.NewError(code, "", 3, map[string]any{"facts": map[string]any{"port": port}})
				observeRunFailure(ctx, e)
				facts := e.Details["facts"].(map[string]any)
				observation.PortObservation = facts["port_observation"].(map[string]any)
				if listener, ok := facts["listener"].(ListenerObservation); ok {
					observation.Listener = listener
				}
			}
		}
		result = append(result, observation)
	}
	return result, nil
}
