package tasks

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

type EditEvaluation struct {
	Event     Event
	Result    Object
	Candidate *State
}

func copyObject(v Object) Object {
	raw, _ := json.Marshal(v)
	var out Object
	_ = json.Unmarshal(raw, &out)
	return out
}

// EvaluateEdit is pure. Allocations supplied by the commit path bind new items
// to UUIDs; nil allocations leave stable request-local identifiers in previews.
func EvaluateEdit(before *State, target string, body Object, allocations map[int]string) (*EditEvaluation, *protocol.Error) {
	ops, e := editOperations(copyObject(body))
	if e != nil {
		return nil, e
	}
	w, e := before.Get(target, "workstream")
	if e != nil {
		return nil, e
	}
	revision := before.Revision + 1
	if before.Version == 1 {
		revision++
	}
	s := before.clone()
	if s.Version == 1 {
		s.applyUpgrade(s.upgradeEvent())
		before = s.clone()
	}
	w = s.Items[w.ID]
	wb := s.definition(w.ID)
	refs := map[string]string{}
	created := map[int]string{}
	lifecycle := map[string]string{}
	changes := []Object{}
	effects := []Object{}
	explicit := map[string]map[string]bool{}
	mark := func(id, field string) {
		if explicit[id] == nil {
			explicit[id] = map[string]bool{}
		}
		explicit[id][field] = true
	}
	for _, op := range ops {
		if op.Name != "task.add" && op.Name != "validation.add" {
			continue
		}
		ref := str(op.Body, "ref")
		if ref != "" {
			if !key.MatchString(ref) || refs["@"+ref] != "" {
				return nil, editError(op.Index, "Invalid or duplicate ref.")
			}
		}
		id := previewID(op)
		if allocations != nil {
			id = allocations[op.Index]
			if !validID(id) {
				return nil, editError(op.Index, "Missing allocated UUID.")
			}
		}
		if s.Items[id] != nil {
			return nil, editError(op.Index, "Duplicate new item identity.")
		}
		if ref != "" {
			refs["@"+ref] = id
		}
		created[op.Index] = id
		kind := strings.Split(op.Name, ".")[0]
		s.Items[id] = &Item{ID: id, Kind: kind, State: "open", Props: Object{}, Depends: []string{}, Order: revision, Revision: revision}
		if kind == "task" {
			s.Items[id].Workstream = target
		}
		s.Tracking[id] = newDefinitionBasis(revision)
		lifecycle[id] = "add"
	}
	for _, op := range ops {
		m := op.Body
		name := op.Name
		parts := strings.Split(name, ".")
		kind, action := parts[0], parts[1]
		id := target
		var item *Item
		if (kind == "task" || kind == "validation") && action == "add" {
			id = created[op.Index]
			item = s.Items[id]
		} else if ref := str(m, "id"); ref != "" {
			var ok bool
			id, ok = referenceID(ref, refs)
			if !ok {
				return nil, editError(op.Index, "Unknown item reference.")
			}
			item = s.Items[id]
			if item == nil {
				return nil, conflict("not_found", []string{id})
			}
			if item.Kind != kind {
				return nil, editError(op.Index, "Reference kind does not match operation.")
			}
			owner := item.Workstream
			if item.Kind == "validation" {
				owner = s.validationWorkstream(item)
			}
			if owner != target {
				return nil, editError(op.Index, "Item belongs to another workstream.")
			}
		}
		if contains([]string{"remove", "restore"}, action) {
			identity := id
			if kind == "requirement" || kind == "acceptance" {
				identity = kind + ":" + str(m, "key")
			}
			if old := lifecycle[identity]; old != "" && old != action {
				return nil, editError(op.Index, "Conflicting lifecycle operations for the same item.")
			}
			lifecycle[identity] = action
		}
		change := Object{"operation_index": op.Index, "op": name, "target_id": id}
		if k := str(m, "key"); k != "" {
			change["key"] = k
		}
		changes = append(changes, change)
		switch {
		case name == "spec.update" || name == "plan.update":
			v, err := operationValue(op, refs)
			if err != nil {
				return nil, err
			}
			doc := objectValue(w.Props[kind])
			doc["body"] = v["body"]
			w.Props[kind] = doc
		case name == "workstream.update":
			v, err := operationValue(op, refs)
			if err != nil {
				return nil, err
			}
			for field, x := range v {
				w.Props[field] = x
				mark(target, field)
			}
			w.Title = str(w.Props, "title")
			w.Description = str(w.Props, "description")
		case kind == "requirement" || kind == "acceptance":
			doc := objectValue(w.Props["spec"])
			field := "acceptance"
			if kind == "requirement" {
				field = "requirements"
			}
			entries := objects(doc, field)
			k := str(m, "key")
			var v Object
			if action == "add" || action == "update" {
				var err *protocol.Error
				v, err = operationValue(op, refs)
				if err != nil {
					return nil, err
				}
				if action == "add" {
					k = str(v, "key")
				}
			}
			if !key.MatchString(k) {
				return nil, editError(op.Index, "Invalid document key.")
			}
			change["key"] = k
			identity := kind + ":" + k
			found := keyed(entries, k)
			removed := wb.RemovedKeys[identity]
			switch action {
			case "add":
				if found != nil || removed != nil || wb.KeyEpochs[identity] != 0 {
					return nil, editError(op.Index, "Document key already exists in history.")
				}
				entries = append(entries, v)
				lifecycle[identity] = "add"
				mark(identity, "requirement_keys")
			case "update":
				if found == nil {
					found = removed
				}
				if found == nil {
					return nil, editError(op.Index, "Document key does not exist.")
				}
				for f, x := range v {
					found[f] = x
					mark(identity, f)
				}
			case "remove":
				if found == nil && removed == nil {
					return nil, editError(op.Index, "Document key does not exist.")
				}
				if found != nil {
					wb.RemovedKeys[identity] = copyObject(found)
					kept := []Object{}
					for _, entry := range entries {
						if str(entry, "key") != k {
							kept = append(kept, entry)
						}
					}
					entries = kept
				}
			case "restore":
				if found == nil {
					if removed == nil {
						return nil, editError(op.Index, "Document key does not exist.")
					}
					entries = append(entries, copyObject(removed))
					delete(wb.RemovedKeys, identity)
				}
			}
			doc[field] = entries
			w.Props["spec"] = doc
		case name == "task.add" || name == "validation.add" || name == "task.update" || name == "validation.update":
			v, err := operationValue(op, refs)
			if err != nil {
				return nil, err
			}
			if name == "task.add" {
				v["workstream_id"] = target
				item.Workstream = target
				wb.Order = append(wb.Order, id)
			}
			for field, x := range v {
				item.Props[field] = x
				mark(id, field)
			}
			item.Title = str(item.Props, "title")
			item.Description = str(item.Props, "description")
		case name == "task.remove" || name == "validation.remove":
			if name == "validation.remove" && !s.definition(id).Removed && item.Props["required"] != false {
				effects = append(effects, Object{"target_id": id, "field": "required_validation", "before": "included", "after": "removed"})
			}
			s.definition(id).Removed = true
		case name == "task.restore" || name == "validation.restore":
			s.definition(id).Removed = false
		case name == "task.depends.set" || name == "workstream.depends.set":
			if kind == "workstream" {
				item = w
			}
			ids := []string{}
			for _, ref := range arr(m, "depends_on") {
				value, ok := referenceID(ref, refs)
				if !ok {
					return nil, editError(op.Index, "Invalid dependency reference.")
				}
				ids = append(ids, value)
			}
			item.Depends = unique(ids)
			mark(id, "depends_on")
		case name == "task.move":
			beforeID, afterID := str(m, "before_id"), str(m, "after_id")
			if (beforeID == "") == (afterID == "") {
				return nil, editError(op.Index, "Choose before_id or after_id.")
			}
			anchor := beforeID
			if anchor == "" {
				anchor = afterID
			}
			resolved, ok := referenceID(anchor, refs)
			if !ok || resolved == id || s.Items[resolved] == nil || s.Items[resolved].Workstream != target || s.Items[resolved].Kind != "task" {
				return nil, editError(op.Index, "Invalid task order anchor.")
			}
			order := []string{}
			for _, x := range wb.Order {
				if x != id {
					order = append(order, x)
				}
			}
			at := -1
			for n, x := range order {
				if x == resolved {
					at = n
					break
				}
			}
			if at < 0 {
				return nil, editError(op.Index, "Task anchor is not in the plan.")
			}
			if afterID != "" {
				at++
			}
			order = append(order, "")
			copy(order[at+1:], order[at:])
			order[at] = id
			wb.Order = order
			mark(id, "move")
			mark(resolved, "move")
		}
	}
	// Prune inherited edges only. Explicit references to removed targets are
	// errors, independent of their position relative to remove operations.
	spec := objectValue(w.Props["spec"])
	for _, ac := range objects(spec, "acceptance") {
		kept := []string{}
		for _, k := range arr(ac, "requirement_keys") {
			if keyed(objects(spec, "requirements"), k) != nil {
				kept = append(kept, k)
			} else if explicit["acceptance:"+str(ac, "key")]["requirement_keys"] {
				return nil, failure("invalid_argument", "Explicit requirement reference is not included.")
			}
		}
		ac["requirement_keys"] = kept
	}
	for _, i := range s.List("") {
		if i.Kind == "workstream" {
			continue
		}
		owner := i.Workstream
		if i.Kind == "validation" {
			o := s.owner(i)
			if o == nil {
				return nil, failure("invalid_argument", "Validation owner does not exist.")
			}
			owner = s.validationWorkstream(i)
			if (str(i.Props, "task_id") != "" && o.Kind != "task") || (str(i.Props, "workstream_id") != "" && o.Kind != "workstream") {
				return nil, failure("invalid_argument", "Invalid validation owner kind.")
			}
		}
		if owner != target {
			if _, isNew := createdIndex(created, i.ID); isNew {
				return nil, failure("invalid_argument", "New item is outside selected workstream.")
			}
			continue
		}
		if explicit[i.ID]["move"] && !s.Included(i) {
			return nil, failure("invalid_argument", "Task order uses an excluded task.")
		}
		keys := []string{}
		for _, k := range arr(i.Props, "acceptance_keys") {
			if keyed(objects(spec, "acceptance"), k) != nil && (s.Included(i) || i.Kind == "validation" || explicit[i.ID]["acceptance_keys"] && lifecycle[i.ID] != "remove") {
				keys = append(keys, k)
			} else if explicit[i.ID]["acceptance_keys"] {
				return nil, failure("invalid_argument", "Explicit acceptance reference is not included.")
			}
		}
		if hash(keys) != hash(arr(i.Props, "acceptance_keys")) {
			effects = append(effects, Object{"target_id": i.ID, "field": "acceptance_keys", "before": arr(i.Props, "acceptance_keys"), "after": keys})
			i.Props["acceptance_keys"] = keys
		}
		if i.Kind == "task" {
			deps := []string{}
			for _, id := range i.Depends {
				p := s.Items[id]
				if p == nil || p.Kind != "task" || p.Workstream != target {
					return nil, failure("invalid_argument", "Dependency must be a task in this workstream.")
				}
				if s.Included(i) && s.Included(p) {
					deps = append(deps, id)
				} else if explicit[i.ID]["depends_on"] {
					return nil, failure("invalid_argument", "Explicit dependency is excluded.")
				}
			}
			if hash(deps) != hash(i.Depends) {
				effects = append(effects, Object{"target_id": i.ID, "field": "depends_on", "before": i.Depends, "after": deps})
				i.Depends = deps
			}
		}
	}
	for _, id := range w.Depends {
		if p := s.Items[id]; p == nil || p.Kind != "workstream" {
			return nil, failure("invalid_argument", "Workstream prerequisite is missing.")
		}
	}
	for _, kind := range []string{"task", "workstream"} {
		items := s.List(kind)
		if len(items) > 0 && topological(items) == nil {
			return nil, conflict("dependency_conflict", []string{target})
		}
	}
	plan := objectValue(w.Props["plan"])
	plan["task_ids"] = []string{}
	plan["validation_ids"] = []string{}
	taskIDs, valIDs := []string{}, []string{}
	for _, i := range s.List("task") {
		if i.Workstream == target && s.Included(i) {
			taskIDs = append(taskIDs, i.ID)
		}
	}
	for _, v := range s.List("validation") {
		if s.validationWorkstream(v) == target && s.Included(v) {
			valIDs = append(valIDs, v.ID)
		}
	}
	plan["task_ids"], plan["validation_ids"] = unique(taskIDs), unique(valIDs)
	w.Props["plan"] = plan
	// Apply semantic epochs once per item, using the final candidate definition.
	for _, i := range s.List("") {
		old := before.Items[i.ID]
		b := s.definition(i.ID)
		if i.Kind == "validation" && old != nil && (old.Props["required"] != false) != (i.Props["required"] != false) {
			effects = append(effects, Object{"target_id": i.ID, "field": "required", "before": old.Props["required"] != false, "after": i.Props["required"] != false})
		}
		if old == nil || hash(before.ownDefinition(old)) != hash(s.ownDefinition(i)) || before.definition(i.ID).Removed != b.Removed {
			b.Epoch = revision
		}
	}
	oldw := before.Items[target]
	for _, doc := range []string{"spec", "plan"} {
		if hash(objectValue(oldw.Props[doc])["body"]) != hash(objectValue(w.Props[doc])["body"]) {
			wb.BodyEpochs[doc] = revision
		}
	}
	oldSpec := objectValue(oldw.Props["spec"])
	for _, field := range []string{"requirements", "acceptance"} {
		prefix := "acceptance:"
		if field == "requirements" {
			prefix = "requirement:"
		}
		for _, v := range append(objects(oldSpec, field), objects(spec, field)...) {
			k := str(v, "key")
			if hash(keyed(objects(oldSpec, field), k)) != hash(keyed(objects(spec, field), k)) {
				wb.KeyEpochs[prefix+k] = revision
			}
		}
	}
	s.assessments = nil
	impact := DefinitionChanges(before, s)
	for _, id := range impact {
		if i := s.Items[id]; i != nil && i.Kind == "workstream" && before.Assessment(id).CompletionStatus == "current" {
			s.definition(id).CloseEpoch = revision
		}
	}
	s.assessments = nil
	patches := []Object{}
	for _, i := range s.List("") {
		old := before.Items[i.ID]
		props := Object{}
		for k, v := range i.Props {
			if old == nil || hash(old.Props[k]) != hash(v) {
				props[k] = v
			}
		}
		if old != nil && hash(before.ownDefinition(old)) == hash(s.ownDefinition(i)) && old.Title == i.Title && len(props) == 0 && hash(before.Tracking[i.ID]) == hash(s.Tracking[i.ID]) {
			continue
		}
		patches = append(patches, Object{"id": i.ID, "kind": i.Kind, "title": i.Title, "description": i.Description, "workstream_id": i.Workstream, "depends_on": i.Depends, "props": props, "basis": s.Tracking[i.ID], "order": i.Order})
	}
	result := Object{"target_id": target, "dry_run": allocations == nil, "would_change": len(patches) > 0, "changes": changes, "effects": effects, "impact": Object{"affected_ids": impact}, "issues": s.EditIssues(w), "next_actions": []Object{}, "created_refs": Object{}, "created_items": []Object{}}
	result["impact"].(Object)["causes"] = editImpactPaths(before, s, changes, impact)
	if len(patches) > 0 {
		next := []Object{{"argv": []string{"devtools", "task", "workstream", "check", target}, "required_inputs": []string{"profile"}, "message": "Inspect coverage and current completion before execution or closure."}}
		for _, id := range impact {
			i := s.Items[id]
			if i == nil || i.Kind != "task" {
				continue
			}
			a := s.Assessment(id)
			if a.ExecutionStatus == "stale" {
				next = append(next, Object{"argv": []string{"devtools", "task", "sync", id}, "required_inputs": []string{"profile", "context", "if-revision", "request-id", "reason"}, "message": "Review the changed definition before syncing this run."})
			} else if a.ExecutionStatus == "removed" {
				next = append(next, Object{"argv": []string{"devtools", "task", "release", s.Current(id).ID}, "required_inputs": []string{"profile", "context", "request-id"}, "message": "Checkpoint and release excluded work when its execution is finished."})
			} else if a.Ready {
				next = append(next, Object{"argv": []string{"devtools", "task", "claim", id}, "required_inputs": []string{"profile", "request-id"}, "message": "Claim this task when ready to implement its current definition."})
			}
		}
		result["next_actions"] = next
	}
	for ref, id := range refs {
		result["created_refs"].(Object)[strings.TrimPrefix(ref, "@")] = id
	}
	for _, op := range ops {
		if id := created[op.Index]; id != "" {
			result["created_items"] = append(result["created_items"].([]Object), Object{"operation_index": op.Index, "id": id})
		}
	}
	if len(patches) == 0 {
		result["changes"] = []Object{}
		result["effects"] = []Object{}
	}
	raw, _ := json.Marshal(result)
	if len(raw) > 2<<20 {
		return nil, failure("invalid_argument", "Edit result exceeds 2 MiB; split the request.")
	}
	return &EditEvaluation{Candidate: s, Event: Event{Action: "workstream.edited", Target: target, Data: Object{"reason": body["reason"], "patches": patches, "effects": effects, "affected_ids": impact}}, Result: result}, nil
}

func createdIndex(created map[int]string, id string) (int, bool) {
	for n, x := range created {
		if id == x {
			return n, true
		}
	}
	return 0, false
}

func (s *State) applyEdit(e Event) {
	if s.Version != JournalVersion {
		panic("edit requires v2")
	}
	for _, p := range objects(e.Data, "patches") {
		id := str(p, "id")
		i := s.Items[id]
		if i == nil {
			i = &Item{ID: id, Kind: str(p, "kind"), State: "open", Props: Object{}, Order: num(p, "order"), Created: e.At}
			s.Items[id] = i
		}
		i.Title = str(p, "title")
		i.Description = str(p, "description")
		i.Workstream = str(p, "workstream_id")
		i.Depends = arr(p, "depends_on")
		i.Updated = e.At
		i.Revision = e.Sequence
		for k, v := range objectValue(p["props"]) {
			i.Props[k] = v
			if k == "spec" || k == "plan" {
				i.Props[k+"_revision"] = e.Sequence
			}
		}
		raw, _ := json.Marshal(p["basis"])
		var b DefinitionBasis
		if json.Unmarshal(raw, &b) != nil {
			panic("invalid edit basis")
		}
		s.Tracking[id] = &b
	}
	s.assessments = nil
}

func (s *State) EditIssues(w *Item) []Object {
	out := []Object{}
	add := func(id, msg string) {
		out = append(out, Object{"code": "coverage", "target_id": id, "message": msg, "blocks": []string{"close"}})
	}
	spec := objectValue(w.Props["spec"])
	if strings.TrimSpace(str(spec, "body")) == "" || strings.TrimSpace(str(objectValue(w.Props["plan"]), "body")) == "" {
		add(w.ID, "Provide specification and plan bodies.")
	}
	if len(objects(spec, "requirements")) == 0 || len(objects(spec, "acceptance")) == 0 {
		add(w.ID, "Define requirements and acceptance criteria.")
	}
	for _, r := range objects(spec, "requirements") {
		found := false
		for _, a := range objects(spec, "acceptance") {
			if contains(arr(a, "requirement_keys"), str(r, "key")) {
				found = true
			}
		}
		if !found {
			add(w.ID, "Requirement has no acceptance criterion: "+str(r, "key"))
		}
	}
	for _, a := range objects(spec, "acceptance") {
		found := false
		for _, t := range s.List("task") {
			if t.Workstream == w.ID && s.Included(t) && contains(arr(t.Props, "acceptance_keys"), str(a, "key")) {
				found = true
			}
		}
		if !found {
			add(w.ID, "Acceptance has no task: "+str(a, "key"))
		}
	}
	count := 0
	for _, t := range s.List("task") {
		if t.Workstream != w.ID || !s.Included(t) {
			continue
		}
		count++
		if t.State == "canceled" {
			continue
		}
		for _, b := range s.Assessment(t.ID).Blockers {
			if str(b, "code") == "coverage" {
				entry := copyObject(b)
				entry["blocks"] = []string{"claim", "sync", "close"}
				out = append(out, entry)
			}
		}
	}
	if count == 0 {
		add(w.ID, "Include at least one task.")
	}
	sort.SliceStable(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
	return out
}
