package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

// RestoreSnapshot preserves history while releasing active claims and dropping
// credentials/retry receipts that belong to the source execution environment.
func RestoreSnapshot(data []byte, source, target string) ([]byte, error) {
	var j Journal
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&j) != nil || d.Decode(new(any)) != io.EOF || j.Version != 1 || j.Profile != source || j.Contexts == nil || j.Receipts == nil {
		return nil, errors.New("invalid task snapshot")
	}
	s := NewState()
	for n, e := range j.Events {
		if e.Sequence != n+1 || safeApply(s, e) != nil {
			return nil, errors.New("invalid task history")
		}
	}
	ids := []string{}
	for id, r := range s.Runs {
		if r.State == "running" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := s.Runs[id]
		e := Event{ID: ID(), Sequence: len(j.Events) + 1, At: stamp(), RequestID: ID(), Action: "run.released", Target: r.TaskID, Data: Object{"run_id": id, "summary": "Released during backup restoration."}}
		j.Events = append(j.Events, e)
	}
	j.Profile = target
	j.Contexts = map[string]string{}
	j.Receipts = map[string]Receipt{}
	return json.Marshal(j)
}
