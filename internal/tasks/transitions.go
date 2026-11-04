package tasks

import "github.com/jinyongp/devtools/internal/protocol"

func (s *State) requireFree(item *Item) *protocol.Error {
	impact := s.Impact(item.ID)
	if len(arr(impact, "running_ids")) > 0 || len(arr(impact, "completed_ids")) > 0 {
		return conflict("transition_conflict", append(arr(impact, "running_ids"), arr(impact, "completed_ids")...))
	}
	return nil
}

func (s *State) requireOpen(item *Item, action string) *protocol.Error {
	if s.Version == JournalVersion && action == "workstream.close" && item.State == "done" && s.definition(item.ID).Activated {
		return nil
	}
	if item.Kind == "task" && item.State != "open" || item.Kind == "workstream" && item.State != "draft" && item.State != "active" {
		return conflict("transition_conflict", []string{item.ID})
	}
	return nil
}
