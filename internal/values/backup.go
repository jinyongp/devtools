package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// InspectSnapshot validates an in-memory profile snapshot using the same
// invariants as normal reads without returning any stored values directly.
func InspectSnapshot(data []byte, profile string) (*State, error) {
	var s State
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF || !s.valid(profile) {
		return nil, errors.New("invalid value snapshot")
	}
	return &s, nil
}

// RestoreSnapshot validates a backup using the same invariants as normal reads.
func RestoreSnapshot(data []byte, source, target string) ([]byte, error) {
	s, err := InspectSnapshot(data, source)
	if err != nil {
		return nil, err
	}
	s.Profile = target
	return json.Marshal(s)
}
