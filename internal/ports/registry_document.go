package ports

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const registryVersion = 4
const registryDocumentLimit = 16 << 20

// Assignment is a joined in-memory/output view. The durable relation stores
// only its owner ID, so profile, directory and alias have one source of truth.
type storedAssignment struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Port       int    `json:"port"`
}

type registryDocument struct {
	Version      int             `json:"version"`
	Instances    []Instance      `json:"instances"`
	Assignments  json.RawMessage `json:"assignments"`
	Reservations []Reservation   `json:"reservations"`
	Network      *NetworkState   `json:"network,omitempty"`
}

func strictJSON(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func (st State) MarshalJSON() ([]byte, error) {
	if st.Version < registryVersion {
		type legacyState State
		return json.Marshal(legacyState(st))
	}
	assignments := make([]storedAssignment, 0, len(st.Assignments))
	for _, a := range st.Assignments {
		assignments = append(assignments, storedAssignment{InstanceID: a.ID, Name: a.Name, Port: a.Port})
	}
	body, err := json.Marshal(assignments)
	if err != nil {
		return nil, err
	}
	return json.Marshal(registryDocument{st.Version, st.Instances, body, st.Reservations, st.Network})
}

func (st *State) UnmarshalJSON(body []byte) error {
	var doc registryDocument
	if err := strictJSON(body, &doc); err != nil {
		return err
	}
	if doc.Version < 1 || doc.Version > registryVersion {
		return fmt.Errorf("unsupported registry version")
	}
	out := State{Version: doc.Version, Instances: doc.Instances, Reservations: doc.Reservations, Network: doc.Network}
	if doc.Version < registryVersion {
		if err := strictJSON(doc.Assignments, &out.Assignments); err != nil {
			return err
		}
	} else {
		var assignments []storedAssignment
		if err := strictJSON(doc.Assignments, &assignments); err != nil {
			return err
		}
		if assignments != nil {
			out.Assignments = make([]Assignment, 0, len(assignments))
		}
		instances := map[string]Instance{}
		for _, i := range out.Instances {
			instances[i.ID] = i
		}
		for _, a := range assignments {
			i, ok := instances[a.InstanceID]
			if !ok {
				return fmt.Errorf("assignment owner missing")
			}
			out.Assignments = append(out.Assignments, Assignment{Instance: i, Name: a.Name, Port: a.Port})
		}
	}
	*st = out
	return nil
}

// Rejoin only at the transaction boundary. Mutators own canonical Instances;
// callers no longer have to synchronize metadata into every service assignment.
func (st *State) joinAssignments() {
	instances := map[string]Instance{}
	for _, i := range st.Instances {
		instances[i.ID] = i
	}
	for n := range st.Assignments {
		if i, ok := instances[st.Assignments[n].ID]; ok {
			st.Assignments[n].Instance = i
		}
	}
}
