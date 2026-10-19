package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/jinyongp/devtools/internal/vault"
	"io"
)

// InspectSnapshot validates an in-memory profile snapshot using the same
// invariants as normal reads without returning any stored values directly.
func InspectSnapshot(data []byte, profile string) (*State, error) {
	if IsLogicalSnapshot(data) {
		material := vault.NewMaterial(data)
		defer material.Destroy()
		view, err := vault.LogicalLegacyView(material, profile)
		if err != nil {
			return nil, err
		}
		defer view.Destroy()
		raw := view.CopyBytes()
		defer clear(raw)
		return InspectSnapshot(raw, profile)
	}
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
	if IsLogicalSnapshot(data) {
		material := vault.NewMaterial(data)
		defer material.Destroy()
		renamed, err := vault.RenameLogical(material, source, target)
		if err != nil {
			return nil, err
		}
		defer renamed.Destroy()
		return renamed.CopyBytes(), nil
	}
	s, err := InspectSnapshot(data, source)
	if err != nil {
		return nil, err
	}
	s.Profile = target
	return json.Marshal(s)
}

// IsLogicalSnapshot identifies the new portable format; callers must still
// validate its complete body. Legacy disk writers must reject it.
func IsLogicalSnapshot(data []byte) bool {
	var header map[string]json.RawMessage
	if json.Unmarshal(data, &header) != nil {
		return false
	}
	_, present := header["logical_values_format"]
	return present
}
