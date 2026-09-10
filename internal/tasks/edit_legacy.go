package tasks

import "github.com/jinyongp/devtools/internal/protocol"

// Legacy definition commands use the same evaluator, retaining their public
// inputs and selected item output. Membership arrays remain assertions, not deletes.
func (s *State) legacyEdit(r Request) (*EditEvaluation, bool, *protocol.Error) {
	if s.Version != JournalVersion {
		return nil, false, nil
	}
	ws := ""
	op := Object{}
	ops := []Object{}
	resultRef := ""
	resultID := r.Target
	i := s.Items[r.Target]
	switch r.Action {
	case "workstream.update":
		ws = r.Target
		op = Object{"op": "workstream.update", "value": r.Body}
	case "task.add":
		ws = str(r.Body, "workstream_id")
		if ws == "" {
			return nil, false, nil
		}
		value := copyObject(r.Body)
		delete(value, "workstream_id")
		op = Object{"op": "task.add", "ref": "item", "value": value}
		resultRef = "item"
	case "task.update", "task.depends":
		if i == nil || i.Workstream == "" {
			return nil, false, nil
		}
		ws = i.Workstream
		if r.Action == "task.update" {
			op = Object{"op": "task.update", "id": i.ID, "value": r.Body}
		} else {
			op = Object{"op": "task.depends.set", "id": i.ID, "depends_on": r.Body["depends_on"]}
		}
	case "validation.add":
		ws = str(r.Body, "workstream_id")
		if ws == "" {
			if owner := s.Items[str(r.Body, "task_id")]; owner != nil {
				ws = owner.Workstream
			}
		}
		if ws == "" {
			return nil, false, nil
		}
		op = Object{"op": "validation.add", "ref": "item", "value": r.Body}
		resultRef = "item"
	case "validation.update":
		if i == nil {
			return nil, false, nil
		}
		ws = s.validationWorkstream(i)
		if ws == "" {
			return nil, false, nil
		}
		op = Object{"op": "validation.update", "id": i.ID, "value": r.Body}
	case "workstream.depends":
		ws = r.Target
		op = Object{"op": "workstream.depends.set", "depends_on": r.Body["depends_on"]}
	case "plan.set":
		ws = r.Target
		taskIDs, valIDs := []string{}, []string{}
		for _, t := range s.List("task") {
			if t.Workstream == ws && s.Included(t) {
				taskIDs = append(taskIDs, t.ID)
			}
		}
		for _, v := range s.List("validation") {
			if s.validationWorkstream(v) == ws && s.Included(v) {
				valIDs = append(valIDs, v.ID)
			}
		}
		if hash(unique(arr(r.Body, "task_ids"))) != hash(unique(taskIDs)) || hash(unique(arr(r.Body, "validation_ids"))) != hash(unique(valIDs)) {
			return nil, true, failure("dependency_conflict", "Plan references must match included membership. Use workstream edit to change scope.")
		}
		op = Object{"op": "plan.update", "value": Object{"body": r.Body["body"]}}
	case "spec.set":
		ws = r.Target
		if e := validateSpec(r.Body); e != nil {
			return nil, true, e
		}
		ops = append(ops, Object{"op": "spec.update", "value": Object{"body": r.Body["body"]}})
		old := objectValue(i.Props["spec"])
		for _, field := range []string{"requirements", "acceptance"} {
			kind := "acceptance"
			if field == "requirements" {
				kind = "requirement"
			}
			for _, entry := range objects(old, field) {
				k := str(entry, "key")
				if keyed(objects(r.Body, field), k) == nil {
					ops = append(ops, Object{"op": kind + ".remove", "key": k})
				}
			}
			for _, entry := range objects(r.Body, field) {
				k := str(entry, "key")
				if keyed(objects(old, field), k) != nil {
					value := copyObject(entry)
					delete(value, "key")
					ops = append(ops, Object{"op": kind + ".update", "key": k, "value": value})
				} else if s.definition(ws).RemovedKeys[kind+":"+k] != nil {
					value := copyObject(entry)
					delete(value, "key")
					ops = append(ops, Object{"op": kind + ".restore", "key": k}, Object{"op": kind + ".update", "key": k, "value": value})
				} else {
					ops = append(ops, Object{"op": kind + ".add", "value": entry})
				}
			}
		}
	default:
		return nil, false, nil
	}
	if op != nil && len(op) > 0 {
		ops = append(ops, op)
	}
	body := copyObject(Object{"reason": "Definition updated through " + r.Action + ".", "operations": ops})
	alloc := map[int]string{}
	for n, op := range ops {
		if str(op, "op") == "task.add" || str(op, "op") == "validation.add" {
			alloc[n] = ID()
		}
	}
	evaluation, e := EvaluateEdit(s, ws, body, alloc)
	if e != nil {
		return nil, true, e
	}
	if resultRef != "" {
		resultID = str(objectValue(evaluation.Result["created_refs"]), resultRef)
	}
	evaluation.Result["target_id"] = resultID
	return evaluation, true, nil
}
