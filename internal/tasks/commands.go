package tasks

import (
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/protocol"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var key = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

func validID(id string) bool { return uuid.MatchString(id) }

// Decode rejects duplicate keys as well as trailing JSON and oversized inputs.
func Decode(data string) (Object, *protocol.Error) {
	if len(data) > 2<<20 || !utf8.ValidString(data) {
		return nil, failure("invalid_argument", "Use a UTF-8 JSON object of at most 2 MiB.")
	}
	d := json.NewDecoder(strings.NewReader(data))
	depth := 0
	var walk func() (any, error)
	walk = func() (any, error) {
		depth++
		defer func() { depth-- }()
		if depth > 64 {
			return nil, io.ErrUnexpectedEOF
		}
		t, e := d.Token()
		if e != nil {
			return nil, e
		}
		switch v := t.(type) {
		case json.Delim:
			switch v {
			case '{':
				m := Object{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return nil, e
					}
					name, ok := k.(string)
					if !ok {
						return nil, io.ErrUnexpectedEOF
					}
					if _, ok = m[name]; ok {
						return nil, io.ErrUnexpectedEOF
					}
					x, e := walk()
					if e != nil {
						return nil, e
					}
					m[name] = x
				}
				_, e = d.Token()
				return m, e
			case '[':
				a := []any{}
				for d.More() {
					x, e := walk()
					if e != nil {
						return nil, e
					}
					a = append(a, x)
				}
				_, e = d.Token()
				return a, e
			}
			return nil, io.ErrUnexpectedEOF
		default:
			return t, nil
		}
	}
	v, e := walk()
	if e != nil {
		return nil, failure("invalid_argument", "Malformed JSON or duplicate object keys.")
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, failure("invalid_argument", "Expected one JSON object.")
	}
	m, ok := v.(Object)
	if !ok {
		return nil, failure("invalid_argument", "Expected a JSON object.")
	}
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &m)
	return m, nil
}

type Definition struct {
	Action         string
	Command        string
	Kind           string
	Read           bool
	Target         bool
	OptionalTarget bool
	Fields         []string
	Required       []string
	Revision       bool
	Context        bool
}

var Definitions = []Definition{
	{Action: "workstream.create", Command: "workstream create", Kind: "workstream", Fields: []string{"title", "description"}, Required: []string{"title"}},
	{Action: "task.add", Command: "add", Kind: "task", Fields: []string{"title", "description", "workstream_id", "acceptance", "acceptance_keys"}, Required: []string{"title"}},
	{Action: "task.update", Command: "update", Kind: "task", Target: true, Fields: []string{"title", "description", "acceptance", "acceptance_keys"}, Revision: true},
	{Action: "spec.set", Command: "workstream spec set", Kind: "workstream", Target: true, Fields: []string{"body", "requirements", "acceptance"}, Required: []string{"body", "requirements", "acceptance"}, Revision: true},
	{Action: "plan.set", Command: "workstream plan set", Kind: "workstream", Target: true, Fields: []string{"body", "task_ids", "validation_ids"}, Required: []string{"body", "task_ids", "validation_ids"}, Revision: true},
	{Action: "task.depends", Command: "depends set", Kind: "task", Target: true, Fields: []string{"depends_on"}, Required: []string{"depends_on"}, Revision: true},
	{Action: "workstream.depends", Command: "workstream depends set", Kind: "workstream", Target: true, Fields: []string{"depends_on"}, Required: []string{"depends_on"}, Revision: true},
	{Action: "task.attach", Command: "attach", Kind: "task", Target: true, Fields: []string{"workstream_id"}, Required: []string{"workstream_id"}, Revision: true},
	{Action: "task.detach", Command: "detach", Kind: "task", Target: true, Revision: true},
	{Action: "task.hold", Command: "hold", Kind: "task", Target: true, Fields: []string{"reason"}, Required: []string{"reason"}, Revision: true},
	{Action: "task.unhold", Command: "unhold", Kind: "task", Target: true, Revision: true},
	{Action: "task.cancel", Command: "cancel", Kind: "task", Target: true, Fields: []string{"reason"}, Required: []string{"reason"}, Revision: true},
	{Action: "task.reopen", Command: "reopen", Kind: "task", Target: true, Fields: []string{"reason"}, Required: []string{"reason"}, Revision: true},
	{Action: "workstream.cancel", Command: "workstream cancel", Kind: "workstream", Target: true, Fields: []string{"reason"}, Required: []string{"reason"}, Revision: true},
	{Action: "workstream.reopen", Command: "workstream reopen", Kind: "workstream", Target: true, Fields: []string{"reason"}, Required: []string{"reason"}, Revision: true},
	{Action: "workstream.activate", Command: "workstream activate", Kind: "workstream", Target: true, Revision: true},
	{Action: "workstream.close", Command: "workstream close", Kind: "workstream", Target: true, Revision: true},
	{Action: "run.claimed", Command: "claim", Kind: "task", OptionalTarget: true},
	{Action: "run.resumed", Command: "resume", Kind: "task", OptionalTarget: true, Context: true},
	{Action: "run.taken_over", Command: "takeover", Kind: "task", Target: true},
	{Action: "run.checkpointed", Command: "checkpoint", Kind: "run", Target: true, Fields: []string{"summary", "decisions", "validation_record_ids", "remaining", "next_action", "blockers"}, Required: []string{"summary"}, Context: true},
	{Action: "run.released", Command: "release", Kind: "run", Target: true, Fields: []string{"summary", "decisions", "validation_record_ids", "remaining", "next_action", "blockers"}, Context: true},
	{Action: "task.completed", Command: "done", Kind: "task", Target: true, Fields: []string{"summary", "validation_record_ids", "commits"}, Required: []string{"summary"}, Context: true},
	{Action: "validation.add", Command: "validation add", Kind: "validation", Fields: []string{"title", "method", "required", "task_id", "workstream_id", "acceptance_keys"}, Required: []string{"title", "method"}},
	{Action: "validation.update", Command: "validation update", Kind: "validation", Target: true, Fields: []string{"title", "method", "required", "acceptance_keys"}, Revision: true},
	{Action: "validation.basis", Command: "validation basis", Kind: "validation", Target: true, Fields: []string{"code"}, Required: []string{"code"}},
	{Action: "validation.record", Command: "validation record", Kind: "validation", Target: true, Fields: []string{"result", "summary", "basis_id", "evidence"}, Required: []string{"result", "summary", "basis_id", "evidence"}},
	{Action: "validation.accept", Command: "validation accept", Kind: "validation", Target: true, Fields: []string{"basis_id", "record_id", "reason"}, Required: []string{"basis_id", "record_id", "reason"}},
	{Action: "validation.waive", Command: "validation waive", Kind: "validation", Target: true, Fields: []string{"reason"}, Required: []string{"reason"}, Revision: true},
	{Action: "validation.unwaive", Command: "validation unwaive", Kind: "validation", Target: true, Fields: []string{"reason"}, Required: []string{"reason"}, Revision: true},
}

func Find(action string) *Definition {
	for n := range Definitions {
		if Definitions[n].Action == action {
			return &Definitions[n]
		}
	}
	return nil
}
func validateBody(def *Definition, b Object) *protocol.Error {
	for k, v := range b {
		if !contains(def.Fields, k) || v == nil {
			return failure("invalid_argument", "Unknown or null input field.")
		}
		switch k {
		case "required":
			if _, ok := v.(bool); !ok {
				return failure("invalid_argument", "Expected a boolean.")
			}
		case "requirements", "code", "evidence", "commits":
			if _, ok := v.([]any); !ok {
				return failure("invalid_argument", "Expected an array.")
			}
		case "acceptance":
			if def.Action == "spec.set" {
				if _, ok := v.([]any); !ok {
					return failure("invalid_argument", "Expected acceptance objects.")
				}
			} else {
				if e := stringArray(v, false); e != nil {
					return e
				}
			}
		case "task_ids", "validation_ids", "depends_on", "validation_record_ids":
			if e := stringArray(v, true); e != nil {
				return e
			}
			b[k] = unique(arr(b, k))
		case "acceptance_keys", "decisions", "remaining", "blockers":
			if e := stringArray(v, false); e != nil {
				return e
			}
			if k == "acceptance_keys" {
				b[k] = unique(arr(b, k))
			}
		default:
			s, ok := v.(string)
			if !ok || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
				return failure("invalid_argument", "Expected a UTF-8 string.")
			}
			max := 16384
			if k == "title" {
				max = 200
			}
			if k == "body" {
				max = 1 << 20
			}
			if len([]rune(s)) > max {
				return failure("invalid_argument", "Input string exceeds its size limit.")
			}
			if k == "body" && len(s) > 1<<20 {
				return failure("invalid_argument", "Document body exceeds 1 MiB.")
			}
			if k != "body" && k != "description" && strings.TrimSpace(s) == "" {
				return failure("invalid_argument", "Expected nonempty input.")
			}
			if strings.HasSuffix(k, "_id") && !validID(s) {
				return failure("invalid_argument", "Expected a UUID.")
			}
		}
	}
	for _, k := range def.Required {
		if _, ok := b[k]; !ok {
			return failure("invalid_argument", "Required input field is missing.")
		}
	}
	if e := nestedBody(def, b); e != nil {
		return e
	}
	if strings.HasSuffix(def.Action, ".update") && len(b) == 0 {
		return failure("invalid_argument", "Provide at least one field to update.")
	}
	return nil
}

func nestedBody(def *Definition, b Object) *protocol.Error {
	for _, field := range []string{"requirements", "acceptance", "code", "evidence", "commits"} {
		value, exists := b[field]
		if !exists || field == "acceptance" && def.Action != "spec.set" {
			continue
		}
		a, ok := value.([]any)
		if !ok {
			return failure("invalid_argument", "Expected object entries.")
		}
		for _, entry := range a {
			m, ok := entry.(map[string]any)
			if !ok {
				return failure("invalid_argument", "Expected object entries.")
			}
			fields := []string{}
			switch field {
			case "requirements":
				fields = []string{"key", "text"}
			case "acceptance":
				fields = []string{"key", "text", "requirement_keys"}
			case "code":
				fields = []string{"repository", "commit", "dirty", "evidence"}
			case "evidence":
				fields = []string{"kind", "reference", "description"}
			case "commits":
				fields = []string{"repository", "commit"}
			}
			if len(m) != len(fields) {
				return failure("invalid_argument", "Nested object fields must match the request schema.")
			}
			for k, v := range m {
				if !contains(fields, k) {
					return failure("invalid_argument", "Unknown nested field.")
				}
				if k == "commit" && field == "code" && v == nil {
					continue
				}
				if k == "dirty" {
					if _, ok := v.(bool); !ok {
						return failure("invalid_argument", "Expected dirty boolean.")
					}
					continue
				}
				if k == "requirement_keys" {
					if e := stringArray(v, false); e != nil {
						return e
					}
					continue
				}
				if k == "evidence" {
					if _, ok := v.(string); ok {
						continue
					}
					if e := stringArray(v, false); e != nil {
						return e
					}
					continue
				}
				s, ok := v.(string)
				if !ok || strings.TrimSpace(s) == "" || len(s) > 16384 || strings.ContainsRune(s, 0) {
					return failure("invalid_argument", "Invalid nested string.")
				}
				if k == "kind" && !contains([]string{"command", "file", "commit", "url", "note"}, s) {
					return failure("invalid_argument", "Invalid evidence kind.")
				}
			}
		}
	}
	return nil
}
func stringArray(v any, ids bool) *protocol.Error {
	a, ok := v.([]any)
	if !ok {
		if ss, yes := v.([]string); yes {
			a = []any{}
			for _, s := range ss {
				a = append(a, s)
			}
		} else {
			return failure("invalid_argument", "Expected an array.")
		}
	}
	for _, x := range a {
		s, ok := x.(string)
		if !ok || strings.TrimSpace(s) == "" || len(s) > 16384 || (ids && !validID(s)) {
			return failure("invalid_argument", "Invalid array entry.")
		}
	}
	return nil
}
func (s *State) prepare(r Request, contexts map[string]string) ([]Event, Object, string, *protocol.Error) {
	def := Find(r.Action)
	if def == nil {
		return nil, nil, "", failure("invalid_argument", "Unknown task action.")
	}
	if r.Body == nil {
		r.Body = Object{}
	}
	if e := validateBody(def, r.Body); e != nil {
		return nil, nil, "", e
	}
	if def.Revision {
		rev, e := strconv.Atoi(r.Options["if-revision"])
		if e != nil || rev < 0 {
			return nil, nil, "", failure("invalid_argument", "Provide if-revision from the latest read.")
		}
		if rev != s.Revision {
			return nil, nil, "", failure("revision_conflict", "Profile changed; read the latest revision.")
		}
	}
	b := r.Body
	target := r.Target
	var i *Item
	var run *Run
	var err *protocol.Error
	contextRun := s.Runs[contexts[hash(r.Options["context"])]]
	if def.Context {
		if contextRun == nil || contextRun.State != "running" {
			return nil, nil, "", failure("context_invalid", "Provide the current execution context.")
		}
		run = contextRun
		if def.Kind == "run" {
			if target != run.ID {
				return nil, nil, "", failure("context_invalid", "Context does not match the requested run.")
			}
			target = run.TaskID
		} else if target == "" {
			target = run.TaskID
		} else if target != run.TaskID {
			return nil, nil, "", failure("context_invalid", "Context does not match this task.")
		}
	}
	if target != "" {
		i, err = s.Get(target, def.Kind)
		if def.Kind == "run" {
			i, err = s.Get(target, "task")
		}
		if err != nil {
			return nil, nil, "", err
		}
	}
	result := Object{}
	credential := ""
	fail := func(e *protocol.Error) ([]Event, Object, string, *protocol.Error) { return nil, nil, "", e }
	ensureFree := func(x *Item) *protocol.Error {
		im := s.Impact(x.ID)
		if len(arr(im, "running_ids")) > 0 || len(arr(im, "completed_ids")) > 0 {
			return conflict("transition_conflict", append(arr(im, "running_ids"), arr(im, "completed_ids")...))
		}
		return nil
	}
	ensureOpen := func(x *Item) *protocol.Error {
		if x.Kind == "task" && x.State != "open" || x.Kind == "workstream" && (x.State != "draft" && x.State != "active") {
			return conflict("transition_conflict", []string{x.ID})
		}
		return nil
	}
	switch r.Action {
	case "task.add", "workstream.create", "validation.add":
		target = ID()
		if r.Action == "task.add" {
			if ws := str(b, "workstream_id"); ws != "" {
				w, e := s.Get(ws, "workstream")
				if e != nil {
					return fail(e)
				}
				if e = ensureOpen(w); e != nil {
					return fail(e)
				}
				if e = s.acceptance(w, arr(b, "acceptance_keys")); e != nil {
					return fail(e)
				}
			} else if len(arr(b, "acceptance_keys")) > 0 {
				return fail(failure("invalid_argument", "Independent tasks use their own acceptance conditions."))
			}
		}
		if r.Action == "validation.add" {
			t, w := str(b, "task_id"), str(b, "workstream_id")
			if (t == "") == (w == "") {
				return fail(failure("invalid_argument", "Choose one validation owner."))
			}
			owner, kind := t, "task"
			if w != "" {
				owner, kind = w, "workstream"
			}
			o, e := s.Get(owner, kind)
			if e != nil {
				return fail(e)
			}
			if e = ensureOpen(o); e != nil {
				return fail(e)
			}
			if s.Current(owner) != nil {
				return fail(conflict("claim_conflict", []string{owner}))
			}
			if e = s.validationKeys(o, arr(b, "acceptance_keys")); e != nil {
				return fail(e)
			}
			if _, ok := b["required"]; !ok {
				b["required"] = true
			}
		}
	case "run.claimed", "run.taken_over":
		if ws := r.Options["workstream"]; ws != "" {
			if _, e := s.Get(ws, "workstream"); e != nil {
				return fail(e)
			}
		}
		if target == "" {
			for _, t := range s.List("task") {
				if ws := r.Options["workstream"]; ws != "" && t.Workstream != ws {
					continue
				}
				if t.State == "open" && s.Current(t.ID) == nil && len(s.Blockers(t)) == 0 {
					i = t
					target = t.ID
					break
				}
			}
			if target == "" {
				return nil, Object{"claimed": false, "run": nil, "context": nil, "context_valid": false, "blockers": []Object{}}, "", nil
			}
		}
		if i.State != "open" || len(s.Blockers(i)) > 0 {
			return fail(conflict("dependency_conflict", []string{i.ID}))
		}
		old := s.Current(target)
		if r.Action == "run.claimed" && old != nil {
			return fail(conflict("claim_conflict", []string{old.ID}))
		}
		if r.Action == "run.taken_over" && (old == nil || old.ID != r.Options["expected-run"]) {
			return fail(conflict("claim_conflict", []string{target}))
		}
		b["run_id"] = ID()
		b["directory"] = r.Options["dir"]
		credential = secret()
		result["claimed"] = true
		result["blockers"] = []Object{}
	case "run.resumed", "run.checkpointed", "run.released", "task.completed":
		if i.State != "open" {
			return fail(conflict("transition_conflict", []string{target}))
		}
		b["run_id"] = run.ID
		if r.Action == "task.completed" {
			if len(s.Blockers(i)) > 0 {
				return fail(conflict("dependency_conflict", []string{i.ID}))
			}
			if e := s.validateCompletion(i); e != nil {
				return fail(e)
			}
		}
	case "task.update", "task.hold", "task.unhold", "task.cancel", "task.depends", "task.attach", "task.detach":
		if e := ensureOpen(i); e != nil {
			return fail(e)
		}
		if e := ensureFree(i); e != nil {
			return fail(e)
		}
		if r.Action == "task.update" {
			if w := s.Items[i.Workstream]; w != nil {
				if e := s.acceptance(w, arr(b, "acceptance_keys")); e != nil {
					return fail(e)
				}
			} else if len(arr(b, "acceptance_keys")) > 0 {
				return fail(failure("invalid_argument", "Independent task cannot reference workstream acceptance."))
			}
		}
		if r.Action == "task.attach" || r.Action == "task.detach" {
			for _, v := range s.List("validation") {
				if str(v.Props, "task_id") == i.ID && len(arr(v.Props, "acceptance_keys")) > 0 {
					return fail(conflict("dependency_conflict", []string{v.ID}))
				}
			}
			if len(i.Depends) > 0 || len(s.successors(i.ID)) > 0 || len(arr(i.Props, "acceptance_keys")) > 0 {
				return fail(conflict("dependency_conflict", []string{i.ID}))
			}
			if w := s.Items[i.Workstream]; w != nil {
				if e := ensureOpen(w); e != nil {
					return fail(e)
				}
			}
			if r.Action == "task.attach" {
				w, e := s.Get(str(b, "workstream_id"), "workstream")
				if e != nil {
					return fail(e)
				}
				if e = ensureOpen(w); e != nil {
					return fail(e)
				}
			}
		}
		if r.Action == "task.depends" {
			if e := s.checkEdges(i, arr(b, "depends_on")); e != nil {
				return fail(e)
			}
		}
	case "task.reopen", "workstream.reopen":
		if i.State != "done" && i.State != "canceled" {
			return fail(conflict("transition_conflict", []string{target}))
		}
		if e := ensureFree(i); e != nil {
			return fail(e)
		}
		if i.Kind == "task" {
			if w := s.Items[i.Workstream]; w != nil {
				if e := ensureOpen(w); e != nil {
					return fail(e)
				}
			}
		}
	case "workstream.depends", "workstream.cancel", "workstream.activate", "workstream.close", "spec.set", "plan.set":
		if e := ensureOpen(i); e != nil {
			return fail(e)
		}
		additive := false
		if r.Action == "plan.set" {
			old, _ := i.Props["plan"].(map[string]any)
			additive = old != nil && str(old, "body") == str(b, "body")
			for _, k := range []string{"task_ids", "validation_ids"} {
				for _, id := range arr(old, k) {
					if !contains(arr(b, k), id) {
						additive = false
					}
				}
			}
		}
		if r.Action != "workstream.close" && r.Action != "workstream.activate" && !additive {
			if e := ensureFree(i); e != nil {
				return fail(e)
			}
		}
		switch r.Action {
		case "workstream.depends":
			if e := s.checkEdges(i, arr(b, "depends_on")); e != nil {
				return fail(e)
			}
		case "workstream.activate":
			if i.State != "draft" {
				return fail(conflict("transition_conflict", []string{target}))
			}
			if issues := s.Check(i); len(issues) > 0 {
				e := failure("validation_required", "Workstream coverage is incomplete.")
				e.Details = map[string]any{"issues": issues}
				return fail(e)
			}
		case "workstream.close":
			if i.State != "active" || len(s.Blockers(i)) > 0 {
				return fail(conflict("transition_conflict", []string{target}))
			}
			if len(s.Check(i)) > 0 {
				return fail(failure("validation_required", "Workstream coverage is incomplete."))
			}
			for _, t := range s.List("task") {
				if t.Workstream == i.ID {
					if t.State == "open" || s.Current(t.ID) != nil {
						return fail(conflict("transition_conflict", []string{t.ID}))
					}
					if t.State == "canceled" && len(arr(t.Props, "acceptance_keys")) > 0 {
						for _, k := range arr(t.Props, "acceptance_keys") {
							covered := false
							for _, other := range s.List("task") {
								if other.Workstream == i.ID && other.State == "done" && contains(arr(other.Props, "acceptance_keys"), k) {
									covered = true
								}
							}
							if !covered {
								return fail(failure("validation_required", "A canceled task's acceptance criterion needs completed coverage."))
							}
						}
					}
				}
				if e := s.validateCompletion(i); e != nil {
					return fail(e)
				}
			}
		case "spec.set":
			if e := validateSpec(b); e != nil {
				return fail(e)
			}
			for _, t := range s.List("task") {
				if t.Workstream == i.ID && t.State == "done" {
					return fail(conflict("transition_conflict", []string{t.ID}))
				}
				if t.Workstream == i.ID {
					for _, k := range arr(t.Props, "acceptance_keys") {
						if !hasKey(objects(b, "acceptance"), k) {
							return fail(conflict("dependency_conflict", []string{t.ID}))
						}
					}
				}
			}
		case "plan.set":
			for _, id := range arr(b, "task_ids") {
				t, e := s.Get(id, "task")
				if e != nil {
					return fail(e)
				}
				if t.Workstream != i.ID {
					return fail(conflict("dependency_conflict", []string{id}))
				}
			}
			for _, id := range arr(b, "validation_ids") {
				v, e := s.Get(id, "validation")
				if e != nil {
					return fail(e)
				}
				if s.validationWorkstream(v) != i.ID {
					return fail(conflict("dependency_conflict", []string{id}))
				}
			}
			if old, ok := i.Props["plan"].(map[string]any); ok && str(old, "body") != str(b, "body") {
				for _, t := range s.List("task") {
					if t.Workstream == i.ID && t.State == "done" {
						return fail(conflict("transition_conflict", []string{t.ID}))
					}
				}
			}
		}
	case "validation.update", "validation.basis", "validation.record", "validation.accept", "validation.waive", "validation.unwaive":
		if e := s.validationAction(i, r.Action, b, r.Options, contextRun); e != nil {
			return fail(e)
		}
	}
	result["target_id"] = target
	if i != nil && (r.Action == "task.update" || r.Action == "validation.update") {
		same := true
		for k, v := range b {
			if hash(i.Props[k]) != hash(v) {
				same = false
			}
		}
		if same {
			return nil, result, "", nil
		}
	}
	if i != nil && (r.Action == "task.unhold" && str(i.Props, "hold") == "" || r.Action == "task.hold" && str(i.Props, "hold") == str(b, "reason")) {
		return nil, result, "", nil
	}
	return []Event{{Action: r.Action, Target: target, Data: b}}, result, credential, nil
}
func (s *State) checkEdges(i *Item, ids []string) *protocol.Error {
	for _, id := range ids {
		p, e := s.Get(id, i.Kind)
		if e != nil {
			return e
		}
		if id == i.ID || contains(s.successors(i.ID), id) || (i.Kind == "task" && (i.Workstream == "" || p.Workstream != i.Workstream)) {
			return conflict("dependency_conflict", []string{i.ID, id})
		}
	}
	return nil
}
func hasKey(a []Object, k string) bool {
	for _, v := range a {
		if str(v, "key") == k {
			return true
		}
	}
	return false
}
func validateSpec(b Object) *protocol.Error {
	req, ac := objects(b, "requirements"), objects(b, "acceptance")
	for _, a := range [][]Object{req, ac} {
		seen := map[string]bool{}
		for _, v := range a {
			k := str(v, "key")
			if !key.MatchString(k) || seen[k] || strings.TrimSpace(str(v, "text")) == "" {
				return failure("invalid_argument", "Invalid or duplicate document key.")
			}
			seen[k] = true
		}
	}
	for _, a := range ac {
		for _, k := range arr(a, "requirement_keys") {
			if !hasKey(req, k) {
				return failure("invalid_argument", "Unknown requirement reference.")
			}
		}
	}
	return nil
}
func (s *State) acceptance(w *Item, keys []string) *protocol.Error {
	spec, _ := w.Props["spec"].(map[string]any)
	for _, k := range keys {
		if !hasKey(objects(spec, "acceptance"), k) {
			return failure("invalid_argument", "Unknown acceptance reference.")
		}
	}
	return nil
}
