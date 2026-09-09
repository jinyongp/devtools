package tasks

import "strings"

// Each affected item gets one bounded representative path and a count of
// contributing operations. Traverse both graphs so removed edges remain visible.
func editImpactPaths(before, after *State, changes []Object, affected []string) []Object {
	edges := map[string][]string{}
	connect := func(from, to string) { edges[from] = append(edges[from], to) }
	for _, s := range []*State{before, after} {
		for _, i := range s.List("") {
			for _, dep := range i.Depends {
				connect(dep, i.ID)
			}
			if i.Kind == "task" {
				if i.Workstream != "" {
					connect(i.ID, i.Workstream)
				}
				if w := s.Items[i.Workstream]; w != nil {
					for _, dep := range w.Depends {
						connect(dep, i.ID)
					}
				}
			}
			if i.Kind == "validation" {
				if owner := s.owner(i); owner != nil {
					connect(i.ID, owner.ID)
				}
			}
		}
	}
	for id, list := range edges {
		edges[id] = unique(list)
	}
	reasons := map[string]Object{}
	for _, change := range changes {
		op := str(change, "op")
		root := str(change, "target_id")
		if op == "task.move" {
			continue
		}
		if strings.HasPrefix(op, "task.") || strings.HasPrefix(op, "validation.") {
			if before.Items[root] != nil && after.Items[root] != nil && hash(before.ownDefinition(before.Items[root])) == hash(after.ownDefinition(after.Items[root])) && before.Included(before.Items[root]) == after.Included(after.Items[root]) {
				continue
			}
		}
		paths := map[string][]string{root: {root}}
		queue := []string{root}
		// Document references are edges only for this document operation; a
		// child changing its owner summary must not affect unrelated siblings.
		if strings.HasPrefix(op, "spec.") || strings.HasPrefix(op, "plan.") || strings.HasPrefix(op, "requirement.") || strings.HasPrefix(op, "acceptance.") || op == "workstream.depends.set" {
			for _, s := range []*State{before, after} {
				w := s.Items[root]
				if w == nil {
					continue
				}
				for _, i := range s.List("") {
					if i.Kind != "task" && i.Kind != "validation" {
						continue
					}
					owner := i.Workstream
					if i.Kind == "validation" {
						owner = s.validationWorkstream(i)
					}
					if owner != root {
						continue
					}
					matches := strings.HasPrefix(op, "spec.") || strings.HasPrefix(op, "plan.") || op == "workstream.depends.set"
					for _, key := range arr(i.Props, "acceptance_keys") {
						if strings.HasPrefix(op, "acceptance.") && key == str(change, "key") {
							matches = true
						}
						ac := keyed(objects(objectValue(w.Props["spec"]), "acceptance"), key)
						if strings.HasPrefix(op, "requirement.") && contains(arr(ac, "requirement_keys"), str(change, "key")) {
							matches = true
						}
					}
					if matches && paths[i.ID] == nil {
						paths[i.ID] = []string{root, i.ID}
						queue = append(queue, i.ID)
					}
				}
			}
		}
		for n := 0; n < len(queue); n++ {
			for _, id := range edges[queue[n]] {
				if paths[id] == nil {
					paths[id] = append(append([]string{}, paths[queue[n]]...), id)
					queue = append(queue, id)
				}
			}
		}
		for _, id := range affected {
			path := paths[id]
			if path == nil {
				continue
			}
			if previous := reasons[id]; previous != nil {
				previous["cause_count"] = num(previous, "cause_count") + 1
				continue
			}
			// Paths are diagnostic summaries, never a truncated impact set.
			length := len(path)
			if length > 32 {
				path = append(append([]string{}, path[:16]...), path[length-16:]...)
			}
			reasons[id] = Object{"target_id": id, "operation_index": change["operation_index"], "path_ids": path, "path_length": length, "truncated": length > 32, "cause_count": 1}
		}
	}
	out := []Object{}
	for _, id := range affected {
		if reason := reasons[id]; reason != nil {
			out = append(out, reason)
		}
	}
	return out
}
