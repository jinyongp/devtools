package tasks

type CompletionFilter struct{ Action, Field, Target, Owner string }

func (s *State) completionIDs(kind, workstream string, filter CompletionFilter) []string {
	if s.readIndex == nil {
		s.prepareReads()
		defer func() { s.readIndex = nil }()
	}
	ids := []string{}
	if kind == "run" {
		for id, run := range s.Runs {
			if filter.Action == "" || run.State == "running" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	for id, item := range s.Items {
		if item.Kind != kind || kind == "task" && workstream != "" && item.Workstream != workstream {
			continue
		}
		if filter.Action == "plan.set" && filter.Field != "" {
			if kind == "task" && item.Workstream != filter.Target || kind == "validation" && s.validationWorkstream(item) != filter.Target || !s.Included(item) {
				continue
			}
		}
		if filter.Action == "validation.add" && filter.Field == "workstream" {
			if owner := s.Items[filter.Owner]; owner != nil && owner.Workstream != id {
				continue
			}
		}
		if filter.Field == "depends-on" {
			if target := s.Items[filter.Target]; target != nil && s.checkEdges(target, []string{id}) != nil {
				continue
			}
		}
		if filter.Field != "" {
			if kind == "workstream" && filter.Action == "run.claimed" {
				ready := false
				for _, task := range s.Items {
					ready = ready || task.Kind == "task" && task.Workstream == id && s.CanClaim(task.ID)
				}
				if !ready {
					continue
				}
			} else if kind == "workstream" && (filter.Action == "task.attach" || filter.Action == "task.add" && s.Version != JournalVersion) && item.State != "draft" && item.State != "active" {
				continue
			}
		} else {
			switch filter.Action {
			case "run.claimed":
				if !s.CanClaim(id) {
					continue
				}
			case "run.taken_over", "run.revoked", "run.resumed", "run.synced":
				if s.Current(id) == nil {
					continue
				}
			case "task.completed":
				if item.State != "open" || s.Current(id) == nil || s.Version == JournalVersion && s.CanComplete(item) != nil || len(s.Blockers(item)) > 0 {
					continue
				}
			case "task.reopen", "workstream.reopen":
				if item.State != "done" && item.State != "canceled" || s.requireFree(item) != nil {
					continue
				}
				if parent := s.Items[item.Workstream]; item.Kind == "task" && parent != nil && s.requireOpen(parent, filter.Action) != nil {
					continue
				}
			case "task.cancel", "task.hold", "task.unhold":
				if s.requireOpen(item, filter.Action) != nil || s.requireFree(item) != nil || filter.Action == "task.unhold" && str(item.Props, "hold") == "" {
					continue
				}
			case "workstream.activate":
				if item.State != "draft" {
					continue
				}
			case "workstream.cancel":
				if s.requireOpen(item, filter.Action) != nil || s.requireFree(item) != nil {
					continue
				}
			case "workstream.close":
				if s.Version == JournalVersion && s.CanClose(item) != nil || s.Version != JournalVersion && (item.State != "active" || len(s.Blockers(item)) > 0) {
					continue
				}
			}
		}
		ids = append(ids, id)
	}
	return ids
}
