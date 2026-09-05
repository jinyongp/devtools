package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// RestoreSnapshot validates a backup using the same invariants as normal reads.
func RestoreSnapshot(data []byte, source, target string) ([]byte, error) {
	var s State
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF || !s.valid(source) {
		return nil, errors.New("invalid value snapshot")
	}
	s.Profile = target
	return json.Marshal(s)
}
