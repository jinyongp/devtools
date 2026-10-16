package tasks

import "sort"

func (s *State) Blockers(i *Item) []Object {
	if s.Version == JournalVersion {
		return s.Assessment(i.ID).Blockers
	}
	out := []Object{}
	add := func(code, id, msg string) { out = append(out, Object{"code": code, "target_id": id, "message": msg}) }
	for _, id := range i.Depends {
		if p := s.Items[id]; p == nil || p.State != "done" {
			add("dependency", id, "A prerequisite is not completed.")
		}
	}
	if h := str(i.Props, "hold"); h != "" {
		add("hold", i.ID, h)
	}
	if i.Kind == "task" && i.Workstream != "" {
		w := s.Items[i.Workstream]
		if w == nil || w.State != "active" {
			add("workstream_inactive", i.Workstream, "Activate the workstream before claiming tasks.")
		}
		if w != nil {
			out = append(out, s.Blockers(w)...)
			if !s.TaskCovered(i) {
				add("plan_incomplete", i.ID, "Connect this task to its plan and validation before claiming.")
			}
		}
	}
	return out
}
func (s *State) successors(id string) []string {
	index := s.readIndex
	if index == nil {
		index = buildTaskReadIndex(s)
	}
	seen := map[string]bool{}
	q := []string{id}
	for len(q) > 0 {
		v := q[0]
		q = q[1:]
		for _, i := range index.dependents[v] {
			if !seen[i.ID] {
				seen[i.ID] = true
				q = append(q, i.ID)
			}
		}
	}
	r := []string{}
	for v := range seen {
		r = append(r, v)
	}
	sort.Strings(r)
	return r
}
func (s *State) Impact(id string) Object {
	target := s.Items[id]
	ids := append([]string{id}, s.successors(id)...)
	runs, done := []string{}, []string{}
	affected := []string{}
	for _, x := range ids {
		affected = append(affected, x)
		i := s.Items[x]
		if i == nil {
			continue
		}
		if x != id && i.State == "done" {
			done = append(done, x)
		}
		if r := s.Current(x); r != nil {
			runs = append(runs, r.ID)
		}
		if i.Kind == "workstream" {
			for _, t := range s.workstreamTasks(x) {
				affected = append(affected, t.ID)
				if r := s.Current(t.ID); r != nil {
					runs = append(runs, r.ID)
				}
				if x != id && t.State == "done" {
					done = append(done, t.ID)
				}
			}

		}
	}
	_ = target
	ts, ws := []string{}, []string{}
	for _, id := range unique(affected) {
		if i := s.Items[id]; i != nil {
			if i.Kind == "task" {
				ts = append(ts, id)
			} else if i.Kind == "workstream" {
				ws = append(ws, id)
			}
		}
	}
	blockers := []Object{}
	for _, id := range runs {
		blockers = append(blockers, Object{"code": "running", "target_id": id, "message": "Release the current run before changing its basis."})
	}
	for _, id := range done {
		blockers = append(blockers, Object{"code": "completed", "target_id": id, "message": "Reopen the completed successor before changing its basis."})
	}
	return Object{"target_ids": []string{id}, "affected_ids": unique(affected), "affected_task_ids": ts, "affected_workstream_ids": ws, "blockers": blockers, "running_ids": unique(runs), "completed_ids": unique(done), "allowed": len(runs) == 0 && len(done) == 0}
}
func (s *State) TaskCovered(t *Item) bool {
	if t.Workstream == "" {
		return true
	}
	w := s.Items[t.Workstream]
	if w == nil {
		return false
	}
	p, _ := w.Props["plan"].(map[string]any)
	if !contains(arr(p, "task_ids"), t.ID) {
		return false
	}
	for _, v := range s.List("validation") {
		if str(v.Props, "task_id") == t.ID && contains(arr(p, "validation_ids"), v.ID) {
			return true
		}
	}
	return false
}
func (s *State) Check(w *Item) []Object {
	if s.Version == JournalVersion {
		return s.EditIssues(w)
	}
	issues := []Object{}
	add := func(id, msg string) {
		issues = append(issues, Object{"code": "coverage", "target_id": id, "message": msg})
	}
	spec, _ := w.Props["spec"].(map[string]any)
	plan, _ := w.Props["plan"].(map[string]any)
	if str(spec, "body") == "" || str(plan, "body") == "" {
		add(w.ID, "Provide specification and plan bodies.")
	}
	req := objects(spec, "requirements")
	ac := objects(spec, "acceptance")
	if len(req) == 0 || len(ac) == 0 {
		add(w.ID, "Define requirements and acceptance criteria.")
	}
	for _, r := range req {
		found := false
		for _, a := range ac {
			if contains(arr(a, "requirement_keys"), str(r, "key")) {
				found = true
			}
		}
		if !found {
			add(w.ID, "A requirement has no acceptance criterion.")
		}
	}
	tasks := []string{}
	vals := []string{}
	for _, t := range s.List("task") {
		if t.Workstream == w.ID {
			tasks = append(tasks, t.ID)
			if !s.TaskCovered(t) {
				add(t.ID, "Task needs plan and validation coverage.")
			}
		}
	}
	if len(tasks) == 0 {
		add(w.ID, "Register at least one task.")
	}
	for _, a := range ac {
		found := false
		for _, id := range tasks {
			if contains(arr(s.Items[id].Props, "acceptance_keys"), str(a, "key")) {
				found = true
			}
		}
		if !found {
			add(w.ID, "An acceptance criterion has no task.")
		}
	}
	for _, v := range s.List("validation") {
		if s.validationWorkstream(v) == w.ID {
			vals = append(vals, v.ID)
		}
	}
	if hash(unique(tasks)) != hash(unique(arr(plan, "task_ids"))) || hash(unique(vals)) != hash(unique(arr(plan, "validation_ids"))) {
		add(w.ID, "Plan references must match its tasks and validations.")
	}
	return issues
}
func (s *State) validationWorkstream(v *Item) string {
	if id := str(v.Props, "task_id"); id != "" {
		if t := s.Items[id]; t != nil {
			return t.Workstream
		}
	}
	return str(v.Props, "workstream_id")
}
func (s *State) Tree(kind, id, ws, direction string, depth, limit int) Object {
	index := s.readIndex
	if index == nil {
		index = buildTaskReadIndex(s)
	}
	graph := graphReader{
		has: func(id string) bool { return s.Items[id] != nil },
		ordered: func() []string {
			ids := []string{}
			for _, item := range index.ordered[kind] {
				if (ws == "" || item.Workstream == ws) && s.Included(item) {
					ids = append(ids, item.ID)
				}
			}
			return ids
		},
		view: func(id string) Object {
			if item := s.Items[id]; item != nil {
				return s.View(item)
			}
			return nil
		},
		neighbors: func(id string) []string {
			if direction == "upstream" {
				return s.Items[id].Depends
			}
			ids := []string{}
			for _, item := range index.dependents[id] {
				if item.Kind == kind {
					ids = append(ids, item.ID)
				}
			}
			return ids
		},
	}
	return graph.tree(id, direction, depth, limit)
}

// graphReader is shared by live and frozen graph queries. Traversal, depth and
// continuation semantics have a single implementation.
type graphReader struct {
	has       func(string) bool
	ordered   func() []string
	view      func(string) Object
	neighbors func(string) []string
}

func (g graphReader) tree(id, direction string, depth, limit int) Object {
	roots := []string{}
	if id != "" {
		roots = append(roots, id)
	} else {
		roots = g.ordered()
	}
	type step struct {
		ID    string
		Depth int
	}
	q := []step{}
	for _, r := range roots {
		q = append(q, step{r, 0})
	}
	seen := map[string]bool{}
	edges := []Object{}
	nodes := []Object{}
	cont := []Object{}
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		if seen[n.ID] {
			continue
		}
		if !g.has(n.ID) {
			continue
		}
		if len(nodes) >= limit {
			cont = append(cont, Object{"id": n.ID, "direction": direction, "depth": depth - n.Depth})
			continue
		}
		seen[n.ID] = true
		nodes = append(nodes, g.view(n.ID))
		if depth == 0 {
			continue
		}
		next := g.neighbors(n.ID)
		for _, x := range next {
			from, to := x, n.ID
			if direction == "downstream" {
				from, to = n.ID, x
			}
			if n.Depth >= depth {
				cont = append(cont, Object{"id": n.ID, "direction": direction, "depth": depth})
				continue
			}
			edges = append(edges, Object{"from": from, "to": to})
			q = append(q, step{x, n.Depth + 1})
		}
	}
	return Object{"nodes": nodes, "edges": edges, "roots": roots, "truncated": len(cont) > 0, "continuations": cont}
}
