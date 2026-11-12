package ports

import (
	"context"
	"encoding/json"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// ProfileRenameFilesHeld projects the new address without reallocating any
// port. The caller holds the exclusive execution and maintenance gates.
func (s Store) ProfileRenameFilesHeld(ctx context.Context, source, target string) (map[string][]byte, *protocol.Error) {
	if source == target || !project.ValidProfile(source) || !project.ValidProfile(target) {
		return nil, fail("invalid_argument")
	}
	st, failure := s.ReadHeld()
	if failure != nil {
		return nil, failure
	}
	targetExists := func() *protocol.Error {
		return protocol.NewError("profile_exists", "The target already has port or proxy state.", 3, nil)
	}
	instances := map[string]bool{}
	for i := range st.Instances {
		instance := &st.Instances[i]
		switch instance.Profile {
		case target:
			return nil, targetExists()
		case source:
			instances[instance.ID] = true
			instance.Profile = target
		}
	}
	for _, assignment := range st.Assignments {
		if instances[assignment.ID] {
			if condition := s.Active(ctx, assignment.ID, assignment.Name); condition != nil {
				return nil, condition
			}
		}
	}
	changed := len(instances) != 0
	if st.Network != nil {
		for _, pin := range st.Network.Pins {
			if pin.Route.Profile == target {
				return nil, targetExists()
			}
			if pin.Route.Profile == source || instances[pin.Route.InstanceID] {
				return nil, fail("pinned")
			}
		}
		for i := range st.Network.Activations {
			activation := &st.Network.Activations[i]
			if activation.Route.Profile == target {
				return nil, targetExists()
			}
			if activation.Route.Profile != source {
				if instances[activation.Route.InstanceID] {
					return nil, fail("invalid_storage")
				}
				continue
			}
			if activation.Mode != "disabled" {
				return nil, protocol.NewError("proxy_active", "Deactivate project proxy routes before renaming the profile.", 3, nil)
			}
			activation.Route.Profile = target
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	st.joinAssignments()
	if !st.valid() {
		return nil, fail("invalid_storage")
	}
	body, err := json.Marshal(st)
	if err != nil || len(body) > registryDocumentLimit {
		return nil, fail("invalid_storage")
	}
	return map[string][]byte{RegistryRelative(s.dataRoot): body}, nil
}
