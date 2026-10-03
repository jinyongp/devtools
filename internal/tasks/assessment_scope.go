package tasks

// assessTargets computes the same definitions as a full assessment pass, but
// only for the selected workstreams and their prerequisite closure. Current
// query state is private to one request; mutation/impact paths keep full-profile
// assessment so changes to external dependents remain visible.
func (s *State) assessTargets(targets []string) map[string]Assessment {
	if s.assessments != nil || s.Version != JournalVersion {
		return s.Assessments()
	}
	members := map[string][]string{}
	validations := map[string][]string{}
	for id, item := range s.Items {
		if item.Kind == "task" && item.Workstream != "" {
			members[item.Workstream] = append(members[item.Workstream], id)
		}
		if item.Kind == "validation" {
			if owner := s.owner(item); owner != nil {
				validations[owner.ID] = append(validations[owner.ID], id)
			}
		}
	}
	items := map[string]*Item{}
	pending := append([]string{}, targets...)
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if items[id] != nil {
			continue
		}
		item := s.Items[id]
		if item == nil {
			continue
		}
		items[id] = item
		pending = append(pending, item.Depends...)
		pending = append(pending, validations[id]...)
		if item.Kind == "workstream" {
			pending = append(pending, members[id]...)
		}
		if item.Kind == "task" && item.Workstream != "" {
			pending = append(pending, item.Workstream)
		}
		if item.Kind == "validation" {
			if owner := s.owner(item); owner != nil {
				pending = append(pending, owner.ID)
			}
		}
	}
	scope := &State{Items: items, Runs: s.Runs, Tracking: s.Tracking, Version: s.Version, Revision: s.Revision}
	return scope.Assessments()
}
