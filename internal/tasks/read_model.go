package tasks

import "sort"

// taskReadIndex belongs to one stable State. It is never copied into a mutable
// preview or persisted; events and materialized state remain authoritative.
type taskReadIndex struct {
	ordered    map[string][]*Item
	dependents map[string][]*Item
	members    map[string][]*Item
	activeRuns map[string]*Run
}

func buildTaskReadIndex(s *State) *taskReadIndex {
	r := &taskReadIndex{
		ordered: map[string][]*Item{}, dependents: map[string][]*Item{},
		members: map[string][]*Item{}, activeRuns: map[string]*Run{},
	}
	all := make([]*Item, 0, len(s.Items))
	for _, item := range s.Items {
		all = append(all, item)
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].Order == all[b].Order {
			return all[a].ID < all[b].ID
		}
		return all[a].Order < all[b].Order
	})
	r.ordered[""] = all
	for _, item := range all {
		r.ordered[item.Kind] = append(r.ordered[item.Kind], item)
		if item.Kind == "task" {
			r.members[item.Workstream] = append(r.members[item.Workstream], item)
		}
		for _, id := range unique(item.Depends) {
			r.dependents[id] = append(r.dependents[id], item)
		}
	}
	for _, run := range s.Runs {
		if run.State == "running" {
			r.activeRuns[run.TaskID] = run
		}
	}
	return r
}

// prepareReads marks the boundary after loading/replay and before query
// evaluation. Mutable candidates use uncached reads, so an edit cannot observe
// indices from an earlier operation in the same transaction.
func (s *State) prepareReads() {
	if s.readIndex == nil {
		s.readIndex = buildTaskReadIndex(s)
	}
}

func (s *State) workstreamTasks(id string) []*Item {
	if s.readIndex != nil {
		return s.readIndex.members[id]
	}
	out := []*Item{}
	for _, item := range s.List("task") {
		if item.Workstream == id {
			out = append(out, item)
		}
	}
	return out
}

func (s *State) downstream(id, kind string) []string {
	index := s.readIndex
	if index == nil {
		index = buildTaskReadIndex(s)
	}
	out := []string{}
	for _, item := range index.dependents[id] {
		if kind == "" || item.Kind == kind {
			out = append(out, item.ID)
		}
	}
	return out
}
