package tasks

import (
	"errors"
	"sort"
)

const contextHistoryLimit = 20

func eventTouches(e Event, ids map[string]bool) bool {
	if ids[e.Target] {
		return true
	}
	if e.Action == "workstream.edited" {
		for _, id := range arr(e.Data, "affected_ids") {
			if ids[id] {
				return true
			}
		}
		for _, patch := range objects(e.Data, "patches") {
			if ids[str(patch, "id")] {
				return true
			}
		}
	}
	return false
}

func eventHistoryIDs(e Event) []string {
	ids := []string{}
	if e.Target != "" {
		ids = append(ids, e.Target)
	}
	if e.Action == "workstream.edited" {
		ids = append(ids, arr(e.Data, "affected_ids")...)
		for _, patch := range objects(e.Data, "patches") {
			if id := str(patch, "id"); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return unique(ids)
}

func (s *State) trackRecentHistory(e Event) {
	ids := eventHistoryIDs(e)
	if len(ids) == 0 {
		return
	}
	if s.HistoryEvents == nil {
		s.HistoryEvents = map[int]Event{}
	}
	if s.HistoryRefs == nil {
		s.HistoryRefs = map[string][]int{}
	}
	if s.historyRefCounts == nil {
		s.historyRefCounts = map[int]int{}
	}
	s.HistoryEvents[e.Sequence] = e
	for _, id := range ids {
		refs := s.HistoryRefs[id]
		if len(refs) > 0 && refs[len(refs)-1] == e.Sequence {
			continue
		}
		refs = append(refs, e.Sequence)
		s.historyRefCounts[e.Sequence]++
		if len(refs) > contextHistoryLimit {
			dropped := refs[0]
			refs = refs[len(refs)-contextHistoryLimit:]
			s.historyRefCounts[dropped]--
			if s.historyRefCounts[dropped] == 0 {
				delete(s.historyRefCounts, dropped)
				delete(s.HistoryEvents, dropped)
			}
		}
		s.HistoryRefs[id] = refs
	}
}

func (s *State) rebuildHistoryCounts() error {
	if s.HistoryEvents == nil || s.HistoryRefs == nil {
		return errors.New("missing recent history")
	}
	counts := map[int]int{}
	for id, refs := range s.HistoryRefs {
		if id == "" || len(refs) > contextHistoryLimit {
			return errors.New("invalid recent history references")
		}
		previous := 0
		for _, sequence := range refs {
			if sequence <= previous || sequence < 1 || sequence > s.Revision {
				return errors.New("invalid recent history sequence")
			}
			event, ok := s.HistoryEvents[sequence]
			if !ok {
				return errors.New("missing recent history event")
			}
			if event.Sequence != sequence || !contains(eventHistoryIDs(event), id) {
				return errors.New("invalid recent history event")
			}
			counts[sequence]++
			previous = sequence
		}
	}
	for sequence := range s.HistoryEvents {
		if counts[sequence] == 0 {
			return errors.New("unreferenced recent history event")
		}
	}
	s.historyRefCounts = counts
	return nil
}

func (s *State) recentHistory(ids map[string]bool) []Event {
	sequences := map[int]bool{}
	for id := range ids {
		for _, sequence := range s.HistoryRefs[id] {
			sequences[sequence] = true
		}
	}
	order := make([]int, 0, len(sequences))
	for sequence := range sequences {
		order = append(order, sequence)
	}
	sort.Ints(order)
	if len(order) > contextHistoryLimit {
		order = order[len(order)-contextHistoryLimit:]
	}
	history := make([]Event, 0, len(order))
	for _, sequence := range order {
		if event, ok := s.HistoryEvents[sequence]; ok {
			history = append(history, event)
		}
	}
	return history
}
