package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"
)

// ArchiveReady only accepts a complete, inactive profile journal. Keeping its
// dependency graph together preserves all cross-item references in the archive.
func ArchiveReady(data []byte, profile string, before time.Time) bool {
	var j Journal
	if json.Unmarshal(data, &j) != nil || j.Profile != profile || len(j.Events) == 0 {
		return false
	}
	s, replayErr := replayJournal(&j)
	if replayErr != nil {
		return false
	}
	last, e := time.Parse(time.RFC3339Nano, j.Events[len(j.Events)-1].At)
	if e != nil || !last.Before(before) {
		return false
	}
	for _, run := range s.Runs {
		if run.State == "running" {
			return false
		}
	}
	for _, item := range s.Items {
		if !s.Included(item) || item.State == "canceled" || item.Kind == "validation" {
			continue
		}
		if item.Kind == "task" && item.Workstream != "" {
			if owner := s.Items[item.Workstream]; owner != nil && owner.State == "canceled" {
				continue
			}
		}
		if item.State != "done" || s.Assessment(item.ID).CompletionStatus != "current" {
			return false
		}
	}
	return true
}

// RestoreSnapshot preserves history while releasing active claims and dropping
// credentials/retry receipts that belong to the source execution environment.
func RestoreSnapshot(data []byte, source, target string) ([]byte, error) {
	var j Journal
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&j) != nil || d.Decode(new(any)) != io.EOF || j.Profile != source || j.Contexts == nil || j.Receipts == nil {
		return nil, errors.New("invalid task snapshot")
	}
	s, replayErr := replayJournal(&j)
	if replayErr != nil {
		return nil, errors.New("invalid task history")
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
