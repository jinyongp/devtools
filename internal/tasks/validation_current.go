package tasks

import (
	"fmt"

	"github.com/jinyongp/devtools/internal/protocol"
)

func (s *State) fingerprintFor(v *Item, ownerSignature string) string {
	return hash(Object{"version": 2, "validation": s.ownDefinition(v), "epoch": s.definition(v.ID).Epoch, "included": s.Included(v), "owner": ownerSignature})
}

func (s *State) basisMatches(v *Item, basis Object, signature string) bool {
	if basis == nil {
		return false
	}
	if s.migrating {
		return s.definition(v.ID).LegacyBases[str(basis, "basis_id")] == str(basis, "fingerprint")
	}
	if num(basis, "definition_version") == 2 {
		return str(basis, "fingerprint") == s.fingerprintFor(v, signature)
	}
	return s.definition(v.ID).LegacyBases[str(basis, "basis_id")] == s.fingerprintFor(v, signature)
}

func (s *State) evidenceCurrent(v *Item, signature, run string) bool {
	id := str(v.Props, "current_basis")
	basis := s.basis(v, id)
	if !s.basisMatches(v, basis, signature) {
		return false
	}
	waiver := objectValue(v.Props["waiver"])
	if str(waiver, "basis_id") == id && (num(waiver, "definition_epoch") == s.definition(v.ID).Epoch || num(basis, "definition_version") != 2 && num(waiver, "definition_revision") == v.Revision) {
		return true
	}
	latest := Object{}
	for _, record := range objects(v.Props, "records") {
		if str(record, "basis_id") == id {
			latest = record
		}
	}
	return str(latest, "result") == "pass" && (run == "" || str(latest, "run_id") == run)
}

func (s *State) validationActionCurrent(v *Item, action string, b Object, opts map[string]string, run *Run) *protocol.Error {
	o := s.owner(v)
	if o == nil {
		return failure("not_found", "Validation owner is missing.")
	}
	signature := s.Assessment(o.ID).Signature
	if action == "validation.update" {
		return s.validationKeys(o, arr(b, "acceptance_keys"))
	}
	if o.Kind == "task" {
		if run == nil || run.State != "running" || run.TaskID != o.ID {
			return failure("context_invalid", "Provide this task's current execution context.")
		}
	} else if o.State != "active" && !(o.State == "done" && s.definition(o.ID).Activated) {
		if action != "validation.record" {
			return conflict("transition_conflict", []string{o.ID})
		}
	}
	if action != "validation.record" && !s.Included(v) {
		return conflict("transition_conflict", []string{v.ID})
	}
	if action == "validation.unwaive" {
		return nil
	}
	if action == "validation.basis" {
		if o.Kind == "workstream" && opts["if-revision"] != fmt.Sprint(s.Revision) {
			return failure("revision_conflict", "Provide if-revision from the observed workstream.")
		}
		if o.Kind == "task" && run.Signature != signature {
			return failure("validation_required", "Sync the current run before creating a new basis.")
		}
		b["basis_id"] = ID()
		b["definition_version"] = 2
		b["fingerprint"] = s.fingerprintFor(v, signature)
		b["definition_signature"] = signature
		if run != nil {
			b["run_id"] = run.ID
		}
		return nil
	}
	id := str(b, "basis_id")
	if action == "validation.waive" {
		id = str(v.Props, "current_basis")
		b["basis_id"] = id
	}
	basis := s.basis(v, id)
	if basis == nil {
		return failure("validation_required", "Choose an existing validation basis.")
	}
	applicable := s.basisMatches(v, basis, signature) && s.Included(v)
	if o.Kind == "workstream" && o.State != "active" && !(o.State == "done" && s.definition(o.ID).Activated) {
		applicable = false
	}
	if action != "validation.record" && !applicable {
		return failure("validation_required", "Create a basis for the current definition.")
	}
	if action == "validation.waive" {
		b["definition_epoch"] = s.definition(v.ID).Epoch
		b["definition_revision"] = v.Revision
		return nil
	}
	if o.Kind == "task" {
		basisRun := str(basis, "run_id")
		if basisRun == "" {
			basisRun = s.definition(v.ID).LegacyRuns[id]
		}
		if basisRun != run.ID {
			return failure("context_invalid", "This basis belongs to another run. Create a current-run basis and explicitly accept matching evidence.")
		}
	}
	if action == "validation.accept" {
		found := false
		for _, record := range objects(v.Props, "records") {
			if str(record, "record_id") != str(b, "record_id") || str(record, "result") != "pass" {
				continue
			}
			old := s.basis(v, str(record, "basis_id"))
			if old != nil && s.basisMatches(v, old, signature) && hash(old["code"]) == hash(basis["code"]) {
				found = true
				b["source_record_id"] = str(record, "record_id")
				b["result"] = "pass"
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
	b["applicable"] = applicable
	if run != nil {
		b["run_id"] = run.ID
	}
	return nil
}

func (s *State) CanComplete(i *Item) *protocol.Error {
	a := s.Assessment(i.ID)
	if !s.Included(i) || a.ExecutionStatus != "current" || len(a.Blockers) > 0 {
		return conflict("transition_conflict", []string{i.ID})
	}
	return s.validateCompletion(i)
}

func (s *State) CanClose(w *Item) *protocol.Error {
	if w.State != "active" && !(w.State == "done" && s.definition(w.ID).Activated) {
		return conflict("transition_conflict", []string{w.ID})
	}
	if len(s.Assessment(w.ID).Blockers) > 0 {
		return conflict("dependency_conflict", []string{w.ID})
	}
	if issues := s.EditIssues(w); len(issues) > 0 {
		e := failure("validation_required", "Workstream coverage is incomplete.")
		e.Details = map[string]any{"issues": issues}
		return e
	}
	for _, t := range s.List("task") {
		if t.Workstream != w.ID {
			continue
		}
		if s.Current(t.ID) != nil {
			return conflict("transition_conflict", []string{t.ID})
		}
		if s.Included(t) && t.State != "canceled" && s.Assessment(t.ID).CompletionStatus != "current" {
			return conflict("transition_conflict", []string{t.ID})
		}
	}
	for _, ac := range objects(objectValue(w.Props["spec"]), "acceptance") {
		covered := false
		for _, t := range s.List("task") {
			if t.Workstream == w.ID && s.Included(t) && s.Assessment(t.ID).CompletionStatus == "current" && contains(arr(t.Props, "acceptance_keys"), str(ac, "key")) {
				covered = true
			}
		}
		if !covered {
			return failure("validation_required", "Acceptance criterion needs current completed coverage: "+str(ac, "key"))
		}
	}
	return s.validateCompletion(w)
}
