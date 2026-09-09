package tasks

import (
	"sort"
	"strings"
)

// Assessment is the single current-definition view used by execution and reads.
// Lifecycle state remains the history of explicit actions.
type Assessment struct {
	Signature        string   `json:"definition_signature"`
	CompletionStatus string   `json:"completion_status"`
	ExecutionStatus  string   `json:"execution_status"`
	NeedsWork        bool     `json:"needs_work"`
	Ready            bool     `json:"ready"`
	Blockers         []Object `json:"blockers"`
}

func (s *State) Included(i *Item) bool {
	if i == nil || s.definition(i.ID).Removed {
		return false
	}
	if i.Kind == "validation" {
		return s.Included(s.owner(i))
	}
	return true
}

func (s *State) lastCompletion(id string) CompletionBasis {
	all := s.definition(id).Completions
	if len(all) == 0 {
		return CompletionBasis{}
	}
	return all[len(all)-1]
}

func (s *State) ownDefinition(i *Item) Object {
	if i == nil {
		return Object{}
	}
	b := Object{"kind": i.Kind, "description": i.Description, "depends": unique(i.Depends), "workstream": i.Workstream}
	for _, key := range []string{"acceptance", "acceptance_keys", "method", "required", "task_id"} {
		if v, ok := i.Props[key]; ok {
			b[key] = v
		}
	}
	return b
}

// topological uses iterative Kahn traversal, including removed nodes so history
// and effects can still refer to them. A cycle is returned as an empty order.
func topological(items []*Item) []*Item {
	byID := map[string]*Item{}
	degree := map[string]int{}
	children := map[string][]string{}
	for _, i := range items {
		byID[i.ID] = i
	}
	for _, i := range items {
		for _, id := range unique(i.Depends) {
			if byID[id] != nil {
				degree[i.ID]++
				children[id] = append(children[id], i.ID)
			}
		}
	}
	queue := []string{}
	for _, i := range items {
		if degree[i.ID] == 0 {
			queue = append(queue, i.ID)
		}
	}
	sort.Strings(queue)
	out := []*Item{}
	for n := 0; n < len(queue); n++ {
		id := queue[n]
		out = append(out, byID[id])
		sort.Strings(children[id])
		for _, next := range children[id] {
			degree[next]--
			if degree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(out) != len(items) {
		return nil
	}
	return out
}

func (s *State) Assessments() map[string]Assessment {
	if s.assessments != nil {
		return s.assessments
	}
	if s.Version == 1 {
		current := s.clone()
		current.applyUpgrade(s.upgradeEvent())
		s.assessments = current.Assessments()
		return s.assessments
	}
	out := map[string]Assessment{}
	validations := map[string][]*Item{}
	tasks := map[string][]*Item{}
	for _, v := range s.List("validation") {
		if o := s.owner(v); o != nil && s.Included(v) {
			validations[o.ID] = append(validations[o.ID], v)
		}
	}
	for _, t := range s.List("task") {
		tasks[t.Workstream] = append(tasks[t.Workstream], t)
	}
	validationDefs := func(id string) []Object {
		defs := []Object{}
		for _, v := range validations[id] {
			defs = append(defs, Object{"id": v.ID, "definition": s.ownDefinition(v), "epoch": s.definition(v.ID).Epoch})
		}
		return defs
	}
	dependency := func(id string) Object {
		i := s.Items[id]
		a := out[id]
		done := s.lastCompletion(id)
		state := "missing"
		if i != nil {
			state = i.State
		}
		return Object{"id": id, "signature": a.Signature, "state": state, "completion": a.CompletionStatus, "result": done.EventID, "close_epoch": s.definition(id).CloseEpoch}
	}
	finish := func(i *Item, signature string, blockers []Object) Assessment {
		b := s.definition(i.ID)
		last := s.lastCompletion(i.ID)
		a := Assessment{Signature: signature, CompletionStatus: "none", ExecutionStatus: "idle", Blockers: blockers}
		if last.Revision > 0 {
			a.CompletionStatus = "stale"
			if i.State == "done" && s.Current(i.ID) == nil && s.Included(i) && last.Revision >= b.CloseEpoch && (last.Signature == "" || last.Signature == signature) {
				a.CompletionStatus = "current"
			}
		}
		if a.CompletionStatus == "current" {
			for _, v := range validations[i.ID] {
				if v.Props["required"] != false && !s.evidenceCurrent(v, signature, "") {
					a.CompletionStatus = "stale"
					break
				}
			}
		}
		if r := s.Current(i.ID); r != nil {
			a.ExecutionStatus = "current"
			if !s.Included(i) {
				a.ExecutionStatus = "removed"
			} else if r.Signature != "" && r.Signature != signature {
				a.ExecutionStatus = "stale"
			}
		}
		a.NeedsWork = s.Included(i) && (i.State == "open" || i.State == "done" && a.CompletionStatus == "stale")
		a.Ready = i.Kind == "task" && a.NeedsWork && s.Current(i.ID) == nil && len(blockers) == 0
		return a
	}
	assessTask := func(t *Item, w *Item) {
		b := s.definition(t.ID)
		definition := Object{"self": s.ownDefinition(t), "epoch": b.Epoch, "included": s.Included(t), "validations": validationDefs(t.ID)}
		blockers := []Object{}
		add := func(code, id, msg string) {
			entry := Object{"code": code, "target_id": id, "message": msg}
			if target := s.Items[id]; target != nil {
				entry["title"] = target.Title
			}
			blockers = append(blockers, entry)
		}
		deps := []Object{}
		for _, id := range unique(t.Depends) {
			deps = append(deps, dependency(id))
			if out[id].CompletionStatus != "current" {
				add("dependency", id, "Complete this prerequisite against its current definition.")
			}
		}
		definition["dependencies"] = deps
		if !s.Included(t) {
			add("removed", t.ID, "Restore this task to execute it.")
		}
		if hold := str(t.Props, "hold"); hold != "" {
			add("hold", t.ID, hold)
		}
		if t.State == "canceled" {
			add("canceled", t.ID, "Reopen this canceled task to execute it.")
		}
		if w != nil {
			wb := s.definition(w.ID)
			spec := objectValue(w.Props["spec"])
			plan := objectValue(w.Props["plan"])
			definition["common"] = Object{"spec": spec["body"], "plan": plan["body"], "epochs": wb.BodyEpochs}
			keys := []Object{}
			for _, key := range unique(arr(t.Props, "acceptance_keys")) {
				ac := keyed(objects(spec, "acceptance"), key)
				keys = append(keys, Object{"key": key, "value": ac, "epoch": wb.KeyEpochs["acceptance:"+key]})
				for _, req := range arr(ac, "requirement_keys") {
					keys = append(keys, Object{"key": req, "value": keyed(objects(spec, "requirements"), req), "epoch": wb.KeyEpochs["requirement:"+req]})
				}
				if ac == nil || len(arr(ac, "requirement_keys")) == 0 {
					add("coverage", t.ID, "Connect this task to a defined requirement and acceptance criterion.")
				}
			}
			// Validation-only acceptance references also constrain its owner's work.
			for _, v := range validations[t.ID] {
				for _, key := range arr(v.Props, "acceptance_keys") {
					ac := keyed(objects(spec, "acceptance"), key)
					keys = append(keys, Object{"validation": v.ID, "key": key, "value": ac, "epoch": wb.KeyEpochs["acceptance:"+key]})
					for _, req := range arr(ac, "requirement_keys") {
						keys = append(keys, Object{"key": req, "value": keyed(objects(spec, "requirements"), req), "epoch": wb.KeyEpochs["requirement:"+req]})
					}
				}
			}
			definition["criteria"] = keys
			wsdeps := []Object{}
			for _, id := range unique(w.Depends) {
				wsdeps = append(wsdeps, dependency(id))
				if out[id].CompletionStatus != "current" {
					add("workstream_dependency", id, "Close the prerequisite workstream against its current scope.")
				}
			}
			definition["workstream_dependencies"] = wsdeps
			if w.State != "active" && !(w.State == "done" && wb.Activated) {
				add("workstream_inactive", w.ID, "Activate this workstream before execution.")
			}
			if strings.TrimSpace(str(spec, "body")) == "" || strings.TrimSpace(str(plan, "body")) == "" {
				add("coverage", w.ID, "Provide specification and plan bodies.")
			}
			if len(arr(t.Props, "acceptance"))+len(arr(t.Props, "acceptance_keys")) == 0 {
				add("coverage", t.ID, "Define this task's acceptance criteria.")
			}
			if len(validations[t.ID]) == 0 {
				add("coverage", t.ID, "Define validation for this task.")
			}
		}
		out[t.ID] = finish(t, hash(definition), blockers)
	}
	for _, t := range topological(tasks[""]) {
		assessTask(t, nil)
	}
	for _, w := range topological(s.List("workstream")) {
		for _, t := range topological(tasks[w.ID]) {
			assessTask(t, w)
		}
		children := []Object{}
		deps := []Object{}
		blockers := []Object{}
		for _, t := range tasks[w.ID] {
			if s.Included(t) {
				children = append(children, dependency(t.ID))
			}
		}
		for _, id := range unique(w.Depends) {
			deps = append(deps, dependency(id))
			if out[id].CompletionStatus != "current" {
				blockers = append(blockers, Object{"code": "dependency", "target_id": id, "title": s.Items[id].Title, "message": "Close the prerequisite workstream against its current scope."})
			}
		}
		b := s.definition(w.ID)
		signature := hash(Object{"definition": s.ownDefinition(w), "epoch": b.Epoch, "body_epochs": b.BodyEpochs, "key_epochs": b.KeyEpochs,
			"spec": w.Props["spec"], "plan_body": objectValue(w.Props["plan"])["body"], "tasks": children, "dependencies": deps, "validations": validationDefs(w.ID)})
		out[w.ID] = finish(w, signature, blockers)
	}
	s.assessments = out
	return out
}

func objectValue(v any) Object {
	switch x := v.(type) {
	case Object:
		return x
	case map[string]any:
		return Object(x)
	}
	return Object{}
}
func keyed(items []Object, key string) Object {
	for _, i := range items {
		if str(i, "key") == key {
			return i
		}
	}
	return nil
}
func (s *State) Assessment(id string) Assessment { return s.Assessments()[id] }
func (s *State) CanClaim(id string) bool         { return s.Assessment(id).Ready }

// DefinitionChanges compares all signatures so external workstream dependents
// are included as well as direct task successors in either graph.
func DefinitionChanges(before, after *State) []string {
	a, b := before.Assessments(), after.Assessments()
	ids := []string{}
	for id, old := range a {
		if next, ok := b[id]; !ok || old.Signature != next.Signature {
			ids = append(ids, id)
		}
	}
	for id := range b {
		if _, ok := a[id]; !ok {
			ids = append(ids, id)
		}
	}
	validationSignature := func(s *State, v *Item, assessments map[string]Assessment) string {
		if v == nil {
			return ""
		}
		owner := s.owner(v)
		if owner == nil {
			return "missing-owner"
		}
		return s.fingerprintFor(v, assessments[owner.ID].Signature)
	}
	seen := map[string]bool{}
	for _, v := range append(before.List("validation"), after.List("validation")...) {
		if seen[v.ID] {
			continue
		}
		seen[v.ID] = true
		if validationSignature(before, before.Items[v.ID], a) != validationSignature(after, after.Items[v.ID], b) {
			ids = append(ids, v.ID)
		}
	}
	return unique(ids)
}
