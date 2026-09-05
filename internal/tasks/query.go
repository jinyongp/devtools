package tasks

import (
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Query struct {
	Command string
	Target  string
	Options map[string]string
	Body    Object
}
type Snapshot struct {
	Fingerprint string    `json:"fingerprint"`
	Expires     time.Time `json:"expires"`
	Revision    int       `json:"revision"`
	Items       []any     `json:"items"`
}

type GraphSnapshot struct {
	Profile string    `json:"profile"`
	Kind    string    `json:"kind"`
	Expires time.Time `json:"expires"`
	Events  []Event   `json:"events"`
}

func (s Store) cacheDirectory() string {
	if s.Cache != "" {
		return s.Cache
	}
	return filepath.Join(s.Directory, "cache")
}

func positive(o map[string]string, k string, def, max int) (int, *protocol.Error) {
	if o[k] == "" {
		return def, nil
	}
	n, e := strconv.Atoi(o[k])
	if e != nil || n < 1 || n > max {
		return 0, failure("invalid_argument", "Numeric option is outside its allowed range.")
	}
	return n, nil
}
func (store Store) Query(q Query) (Object, *protocol.Error) {
	release, gateErr := maintenance.Acquire(context.Background(), maintenance.Root(store.Directory))
	if gateErr != nil {
		return nil, storageError()
	}
	defer release()
	_, s, e := store.load()
	if e != nil {
		return nil, e
	}
	out := Object{"profile": store.Profile, "revision": s.Revision}
	kind := "task"
	cmd := q.Command
	if strings.HasPrefix(cmd, "workstream ") {
		kind = "workstream"
		cmd = strings.TrimPrefix(cmd, "workstream ")
	}
	if strings.HasPrefix(cmd, "validation ") {
		kind = "validation"
		cmd = strings.TrimPrefix(cmd, "validation ")
	}
	if cmd == "tree" && q.Options["cursor"] != "" {
		id := q.Options["cursor"]
		if !validID(id) {
			return nil, failure("cursor_invalid", "Start a new graph query.")
		}
		var snapshot GraphSnapshot
		if e := ReadPrivate(filepath.Join(store.cacheDirectory(), id+".json"), &snapshot); e != nil || snapshot.Profile != store.Profile || snapshot.Kind != kind || time.Now().After(snapshot.Expires) {
			return nil, failure("cursor_invalid", "Start a new graph query.")
		}
		s = NewState()
		for _, event := range snapshot.Events {
			if e := safeApply(s, event); e != nil {
				return nil, storageError()
			}
		}
		out["revision"] = s.Revision
	}
	var item *Item
	if q.Target != "" && q.Command != "checkpoint list" {
		item, e = s.Get(q.Target, kind)
		if e != nil {
			return nil, e
		}
	}
	if item == nil && contains([]string{"show", "spec show", "plan show", "check", "impact", "context", "export"}, cmd) {
		return nil, failure("invalid_argument", "Provide a target UUID.")
	}
	limit, e := positive(q.Options, "limit", 50, 200)
	if e != nil {
		return nil, e
	}
	items := []any{}
	switch cmd {
	case "show":
		out["item"] = s.View(item)
		if kind == "validation" {
			out["bases"] = item.Props["bases"]
			out["records"] = item.Props["records"]
		}
		return out, nil
	case "spec show", "plan show":
		name := strings.Fields(cmd)[0]
		out["item"] = item.Props[name]
		return out, nil
	case "check":
		issues := s.Check(item)
		out["valid"] = len(issues) == 0
		out["issues"] = issues
		return out, nil
	case "impact":
		if q.Body != nil {
			fields := []string{"title", "description", "depends_on"}
			if kind == "task" {
				fields = append(fields, "workstream_id", "acceptance", "acceptance_keys")
			}
			if e := validateBody(&Definition{Action: "preview", Fields: fields}, q.Body); e != nil {
				return nil, e
			}
			if _, ok := q.Body["depends_on"]; ok {
				if e := s.checkEdges(item, arr(q.Body, "depends_on")); e != nil {
					return nil, e
				}
				item.Depends = arr(q.Body, "depends_on")
			}
			if id := str(q.Body, "workstream_id"); id != "" {
				if _, e := s.Get(id, "workstream"); e != nil {
					return nil, e
				}
			}
			out["proposed_changes"] = q.Body
		}
		for k, v := range s.Impact(item.ID) {
			out[k] = v
		}
		return out, nil
	case "tree":
		depth, e := positive(q.Options, "depth", 3, 20)
		if e != nil {
			return nil, e
		}
		direction := q.Options["direction"]
		if direction == "" {
			direction = "upstream"
		}
		if direction != "upstream" && direction != "downstream" {
			return nil, failure("invalid_argument", "Choose upstream or downstream.")
		}
		for k, v := range s.Tree(kind, q.Target, q.Options["workstream"], direction, depth, limit) {
			out[k] = v
		}
		out["cursor"] = nil
		if out["truncated"] == true {
			token := q.Options["cursor"]
			if token == "" {
				dir := store.cacheDirectory()
				if e := PrivateDir(dir); e != nil {
					return nil, storageError()
				}
				token = ID()
				snapshot := GraphSnapshot{Profile: store.Profile, Kind: kind, Expires: time.Now().Add(30 * time.Minute), Events: s.Events}
				if e := WritePrivate(filepath.Join(dir, token+".json"), snapshot); e != nil {
					return nil, storageError()
				}
			}
			out["cursor"] = token
		}
		return out, nil
	case "next":
		out["item"] = nil
		out["reason"] = "No ready task."
		for _, t := range s.List("task") {
			if q.Options["workstream"] != "" && t.Workstream != q.Options["workstream"] {
				continue
			}
			if t.State == "open" && s.Current(t.ID) == nil && len(s.Blockers(t)) == 0 {
				out["item"] = s.View(t)
				out["reason"] = "Oldest ready task."
				break
			}
		}
		return out, nil
	case "context", "export":
		out["item"] = s.View(item)
		out["documents"] = Object{}
		out["tasks"] = []any{}
		out["validations"] = []any{}
		history := []Event{}
		ids := map[string]bool{item.ID: true}
		w := item
		if item.Kind == "task" {
			w = s.Items[item.Workstream]
		}
		if w != nil {
			out["documents"] = Object{"spec": w.Props["spec"], "plan": w.Props["plan"]}
			ids[w.ID] = true
		}
		ts, vs := []any{}, []any{}
		for _, t := range s.List("task") {
			if t.ID == item.ID || item.Kind == "workstream" && t.Workstream == item.ID {
				ts = append(ts, s.View(t))
				ids[t.ID] = true
			}
		}
		for _, v := range s.List("validation") {
			if ids[s.owner(v).ID] {
				vs = append(vs, s.View(v))
				ids[v.ID] = true
			}
		}
		for _, ev := range s.Events {
			if ids[ev.Target] {
				history = append(history, ev)
			}
		}
		out["tasks"] = ts
		out["validations"] = vs
		out["history"] = history
		out["truncated"] = false
		if cmd == "context" {
			if len(history) > 20 {
				out["history"] = history[len(history)-20:]
			}
			b, _ := json.Marshal(out)
			if len(b) > 2<<20 {
				out["documents"] = Object{}
				out["history"] = []Event{}
				out["tasks"] = []any{}
				out["validations"] = []any{}
				out["truncated"] = true
				out["omitted_ids"] = []string{item.ID}
			}
		}
		return out, nil
	case "current":
		dir := q.Options["dir"]
		if dir == "" {
			dir = "."
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return nil, failure("invalid_argument", "Cannot resolve directory.")
		}
		for _, r := range s.Runs {
			if r.State == "running" && r.Directory == dir {
				items = append(items, r)
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].(*Run).Started < items[j].(*Run).Started })
	case "checkpoint list":
		if s.Runs[q.Target] == nil {
			return nil, failure("not_found", "Run does not exist.")
		}
		for _, ev := range s.Events {
			if str(ev.Data, "run_id") == q.Target && contains([]string{"run.checkpointed", "run.released", "task.completed"}, ev.Action) {
				items = append(items, ev)
			}
		}
	case "history":
		ids := map[string]bool{q.Target: true}
		if item != nil {
			for _, t := range s.List("task") {
				if item.Kind == "workstream" && t.Workstream == item.ID {
					ids[t.ID] = true
				}
			}
			for _, v := range s.List("validation") {
				if o := s.owner(v); o != nil && ids[o.ID] {
					ids[v.ID] = true
				}
			}
		}
		for _, ev := range s.Events {
			if q.Target == "" || ids[ev.Target] {
				items = append(items, ev)
			}
		}
	case "list":
		state := q.Options["state"]
		allowed := []string{"open", "done", "canceled", "all"}
		if kind == "workstream" {
			allowed = []string{"draft", "active", "done", "canceled", "all"}
		}
		if state != "" && !contains(allowed, state) {
			return nil, failure("invalid_argument", "Invalid state filter.")
		}
		for _, i := range s.List(kind) {
			if state == "" && (i.State == "done" || i.State == "canceled") {
				continue
			}
			if state != "" && state != "all" && i.State != state {
				continue
			}
			if ws := q.Options["workstream"]; ws != "" && i.Workstream != ws {
				continue
			}
			items = append(items, s.View(i))
		}
	default:
		return nil, failure("invalid_argument", "Unknown task query.")
	}
	return store.page(q, out, items, limit)
}
func (s Store) page(q Query, out Object, items []any, limit int) (Object, *protocol.Error) {
	opts := Object{}
	for k, v := range q.Options {
		if k != "cursor" && k != "limit" {
			opts[k] = v
		}
	}
	fp := hash(Object{"command": q.Command, "target": q.Target, "options": opts, "profile": s.Profile})
	dir := s.Cache
	if dir == "" {
		dir = filepath.Join(s.Directory, "cache")
	}
	offset := 0
	token := ""
	snapshot := Snapshot{Fingerprint: fp, Expires: time.Now().Add(30 * time.Minute), Revision: num(out, "revision"), Items: items}
	if c := q.Options["cursor"]; c != "" {
		parts := strings.Split(c, ":")
		if len(parts) != 2 || !validID(parts[0]) {
			return nil, failure("cursor_invalid", "Start a new query.")
		}
		token = parts[0]
		n, e := strconv.Atoi(parts[1])
		if e != nil || n < 0 {
			return nil, failure("cursor_invalid", "Start a new query.")
		}
		offset = n
		if e = ReadPrivate(filepath.Join(dir, token+".json"), &snapshot); e != nil || snapshot.Fingerprint != fp || time.Now().After(snapshot.Expires) || offset > len(snapshot.Items) {
			return nil, failure("cursor_invalid", "Start a new query.")
		}
		items = snapshot.Items
		out["revision"] = snapshot.Revision
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	out["items"] = items[offset:end]
	out["next_cursor"] = nil
	if end < len(items) {
		if token == "" {
			if e := PrivateDir(dir); e != nil {
				return nil, storageError()
			}
			token = ID()
			if e := WritePrivate(filepath.Join(dir, token+".json"), snapshot); e != nil {
				return nil, storageError()
			}
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if info, e := entry.Info(); e == nil && time.Since(info.ModTime()) > 30*time.Minute && strings.HasSuffix(entry.Name(), ".json") {
					_ = os.Remove(filepath.Join(dir, entry.Name()))
				}
			}
		}
		out["next_cursor"] = token + ":" + strconv.Itoa(end)
	}
	return out, nil
}
