package tasks

import "github.com/jinyongp/devtools/internal/protocol"

func (s *State) owner(v *Item) *Item {
	id := str(v.Props, "task_id")
	if id == "" {
		id = str(v.Props, "workstream_id")
	}
	return s.Items[id]
}
func (s *State) basisFingerprint(v *Item) string {
	o := s.owner(v)
	b := Object{"validation": v.Revision, "owner": o.Revision}
	w := o
	if o.Kind == "task" {
		w = s.Items[o.Workstream]
	}
	if w != nil {
		p, _ := w.Props["plan"].(map[string]any)
		b["spec"] = w.Props["spec_revision"]
		b["plan_body"] = p["body"]
	}
	return hash(b)
}
func (s *State) basis(v *Item, id string) Object {
	all, _ := v.Props["bases"].(map[string]any)
	b, _ := all[id].(map[string]any)
	return b
}
func (s *State) validationAction(v *Item, action string, b Object, opts map[string]string, run *Run) *protocol.Error {
	o := s.owner(v)
	if o == nil {
		return failure("not_found", "Validation owner is missing.")
	}
	if o.State == "done" || o.State == "canceled" {
		return conflict("transition_conflict", []string{o.ID})
	}
	if action == "validation.update" {
		if e := s.validationKeys(o, arr(b, "acceptance_keys")); e != nil {
			return e
		}
		im := s.Impact(o.ID)
		if len(arr(im, "running_ids"))+len(arr(im, "completed_ids")) > 0 {
			return conflict("transition_conflict", arr(im, "affected_ids"))
		}
		return nil
	}
	if action == "validation.unwaive" {
		return nil
	}
	if action == "validation.basis" {
		b["basis_id"] = ID()
		b["fingerprint"] = s.basisFingerprint(v)
		return nil
	}
	id := str(b, "basis_id")
	if action == "validation.waive" {
		id = str(v.Props, "current_basis")
		b["basis_id"] = id
	}
	basis := s.basis(v, id)
	if basis == nil || str(basis, "fingerprint") != s.basisFingerprint(v) {
		return failure("validation_required", "Create a basis for the current definition.")
	}
	if action == "validation.waive" {
		b["definition_revision"] = v.Revision
		return nil
	}
	if o.Kind == "task" && (run == nil || run.State != "running" || run.TaskID != o.ID) {
		return failure("context_invalid", "Provide this task's current execution context.")
	}
	if action == "validation.accept" {
		found := false
		for _, r := range objects(v.Props, "records") {
			if str(r, "record_id") == str(b, "record_id") && str(r, "result") == "pass" {
				old := s.basis(v, str(r, "basis_id"))
				if old != nil && hash(old["code"]) == hash(basis["code"]) && str(old, "fingerprint") == str(basis, "fingerprint") {
					found = true
					b["source_record_id"] = str(b, "record_id")
					b["result"] = "pass"
				}
			}
		}
		if !found {
			return failure("validation_required", "Choose a passing record with matching code and definition.")
		}
	}
	if !contains([]string{"pass", "fail", "blocked", "skipped"}, str(b, "result")) {
		return failure("invalid_argument", "Use pass, fail, blocked, or skipped.")
	}
	b["record_id"] = ID()
	if run != nil {
		b["run_id"] = run.ID
	}
	return nil
}
func (s *State) validationKeys(o *Item, keys []string) *protocol.Error {
	w := o
	if o.Kind == "task" {
		w = s.Items[o.Workstream]
	}
	if w == nil {
		if len(keys) > 0 {
			return failure("invalid_argument", "Independent validation uses its task's acceptance conditions.")
		}
		return nil
	}
	return s.acceptance(w, keys)
}
func (s *State) validateCompletion(o *Item) *protocol.Error {
	for _, v := range s.List("validation") {
		if s.owner(v) != o || v.Props["required"] == false {
			continue
		}
		id := str(v.Props, "current_basis")
		b := s.basis(v, id)
		if b == nil || str(b, "fingerprint") != s.basisFingerprint(v) {
			return conflict("validation_required", []string{v.ID})
		}
		waiver, _ := v.Props["waiver"].(map[string]any)
		if str(waiver, "basis_id") == id && num(waiver, "definition_revision") == v.Revision {
			continue
		}
		latest := Object{}
		for _, r := range objects(v.Props, "records") {
			if str(r, "basis_id") == id {
				latest = r
			}
		}
		if str(latest, "result") != "pass" {
			return conflict("validation_required", []string{v.ID})
		}
		if o.Kind == "task" {
			r := s.Current(o.ID)
			if r == nil || str(latest, "run_id") != r.ID {
				return failure("validation_required", "Accept the previous run's passing evidence in the current run.")
			}
		}
	}
	return nil
}
