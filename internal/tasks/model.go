// Package tasks implements an action journal and its deterministic projection.
package tasks

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

type Object map[string]any
type Event struct {
	ID        string `json:"id"`
	Sequence  int    `json:"sequence"`
	At        string `json:"occurred_at"`
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
	Target    string `json:"target_id"`
	Data      Object `json:"data"`
}
type Item struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	State       string   `json:"state"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Workstream  string   `json:"workstream_id"`
	Depends     []string `json:"depends_on"`
	Props       Object   `json:"properties"`
	Revision    int      `json:"definition_revision"`
	Created     string   `json:"created_at"`
	Updated     string   `json:"updated_at"`
	Order       int      `json:"-"`
}
type Run struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	State      string `json:"state"`
	Previous   string `json:"previous_run_id"`
	Directory  string `json:"directory"`
	Started    string `json:"started_at"`
	Activity   string `json:"last_activity_at"`
	Ended      string `json:"ended_at"`
	Definition int    `json:"definition_revision"`
	Spec       int    `json:"spec_revision"`
	Plan       int    `json:"plan_revision"`
}
type State struct {
	Items    map[string]*Item
	Runs     map[string]*Run
	Events   []Event
	Revision int
}

func (r Run) MarshalJSON() ([]byte, error) {
	type plain Run
	b, e := json.Marshal(plain(r))
	if e != nil {
		return nil, e
	}
	v := Object{}
	if e = json.Unmarshal(b, &v); e != nil {
		return nil, e
	}
	if r.Previous == "" {
		v["previous_run_id"] = nil
	}
	if r.Ended == "" {
		v["ended_at"] = nil
	}
	return json.Marshal(v)
}

func NewState() *State {
	return &State{Items: map[string]*Item{}, Runs: map[string]*Run{}, Events: []Event{}}
}
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func secret() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func str(m Object, k string) string { s, _ := m[k].(string); return s }
func num(m Object, k string) int {
	switch n := m[k].(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}
func arr(m Object, k string) []string {
	a := []string{}
	switch v := m[k].(type) {
	case []string:
		return v
	case []any:
		for _, s := range v {
			if t, ok := s.(string); ok {
				a = append(a, t)
			}
		}
	}
	return a
}
func objects(m Object, k string) []Object {
	result := []Object{}
	switch a := m[k].(type) {
	case []any:
		for _, v := range a {
			if x, ok := v.(map[string]any); ok {
				result = append(result, Object(x))
			}
		}
	case []Object:
		return a
	}
	return result
}
func unique(a []string) []string {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	r := []string{}
	for s := range set {
		r = append(r, s)
	}
	sort.Strings(r)
	return r
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func failure(code, message string) *protocol.Error {
	exit := 3
	if code == "invalid_argument" {
		exit = 2
	}
	return protocol.NewError(code, message, exit, nil)
}
func conflict(code string, ids []string) *protocol.Error {
	e := failure(code, "The current state does not allow this action. Inspect the affected items.")
	e.Details = map[string]any{"ids": unique(ids)}
	return e
}
func (s *State) Get(id, kind string) (*Item, *protocol.Error) {
	i := s.Items[id]
	if i == nil || (kind != "" && i.Kind != kind) {
		return nil, failure("not_found", "The requested item does not exist in this profile.")
	}
	return i, nil
}
func (s *State) Current(id string) *Run {
	for _, r := range s.Runs {
		if r.TaskID == id && r.State == "running" {
			return r
		}
	}
	return nil
}
func (s *State) List(kind string) []*Item {
	r := []*Item{}
	for _, i := range s.Items {
		if kind == "" || i.Kind == kind {
			r = append(r, i)
		}
	}
	sort.Slice(r, func(a, b int) bool { return r[a].Order < r[b].Order })
	return r
}

// Apply only reduces accepted events; authorization and guards precede append.
func (s *State) Apply(e Event) {
	d := e.Data
	i := s.Items[e.Target]
	switch e.Action {
	case "task.add", "workstream.create", "validation.add":
		kind := strings.Split(e.Action, ".")[0]
		state := "open"
		if kind == "workstream" {
			state = "draft"
		}
		i = &Item{ID: e.Target, Kind: kind, State: state, Title: str(d, "title"), Description: str(d, "description"), Workstream: str(d, "workstream_id"), Depends: []string{}, Props: Object{}, Revision: e.Sequence, Created: e.At, Updated: e.At, Order: e.Sequence}
		for k, v := range d {
			i.Props[k] = v
		}
		s.Items[i.ID] = i
	case "task.update", "validation.update":
		for k, v := range d {
			i.Props[k] = v
		}
		if v, ok := d["title"]; ok {
			i.Title = v.(string)
		}
		if v, ok := d["description"]; ok {
			i.Description = v.(string)
		}
		i.Revision = e.Sequence
	case "spec.set", "plan.set":
		name := strings.Split(e.Action, ".")[0]
		i.Props[name] = map[string]any(d)
		i.Props[name+"_revision"] = e.Sequence
	case "task.depends", "workstream.depends":
		i.Depends = arr(d, "depends_on")
		i.Revision = e.Sequence
	case "task.attach", "task.detach":
		old := i.Workstream
		i.Workstream = str(d, "workstream_id")
		i.Props["workstream_id"] = i.Workstream
		i.Revision = e.Sequence
		for _, w := range []string{old, i.Workstream} {
			if ws := s.Items[w]; ws != nil {
				if p, ok := ws.Props["plan"].(map[string]any); ok {
					ids := []string{}
					for _, id := range arr(p, "task_ids") {
						if id != i.ID {
							ids = append(ids, id)
						}
					}
					if w == i.Workstream {
						ids = append(ids, i.ID)
					}
					p["task_ids"] = unique(ids)
					vals := []string{}
					for _, id := range arr(p, "validation_ids") {
						v := s.Items[id]
						if v == nil || str(v.Props, "task_id") != i.ID {
							vals = append(vals, id)
						}
					}
					if w == i.Workstream {
						for _, v := range s.List("validation") {
							if str(v.Props, "task_id") == i.ID {
								vals = append(vals, v.ID)
							}
						}
					}
					p["validation_ids"] = unique(vals)
					ws.Props["plan_revision"] = e.Sequence
				}
			}
		}
	case "task.hold":
		i.Props["hold"] = str(d, "reason")
	case "task.unhold":
		delete(i.Props, "hold")
	case "task.cancel":
		i.State = "canceled"
		i.Props["reason"] = str(d, "reason")
	case "task.reopen":
		i.State = "open"
		i.Revision = e.Sequence
		delete(i.Props, "hold")
	case "workstream.activate":
		i.State = "active"
	case "workstream.close":
		i.State = "done"
		i.Props["result"] = d
	case "workstream.reopen":
		i.State = "draft"
	case "workstream.cancel":
		i.State = "canceled"
		for _, t := range s.Items {
			if t.Kind == "task" && t.Workstream == i.ID && t.State == "open" {
				t.State = "canceled"
				t.Updated = e.At
			}
		}
	case "run.claimed", "run.taken_over":
		previous := s.Current(e.Target)
		prev := ""
		if previous != nil {
			prev = previous.ID
			previous.State = "taken_over"
			previous.Ended = e.At
			previous.Activity = e.At
		}
		r := &Run{ID: str(d, "run_id"), TaskID: e.Target, State: "running", Previous: prev, Directory: str(d, "directory"), Started: e.At, Activity: e.At, Definition: i.Revision}
		if w := s.Items[i.Workstream]; w != nil {
			r.Spec = num(w.Props, "spec_revision")
			r.Plan = num(w.Props, "plan_revision")
		}
		s.Runs[r.ID] = r
	case "run.resumed", "run.checkpointed":
		s.Runs[str(d, "run_id")].Activity = e.At
	case "run.released", "task.completed":
		r := s.Runs[str(d, "run_id")]
		r.Activity = e.At
		r.Ended = e.At
		r.State = "released"
		if e.Action == "task.completed" {
			r.State = "completed"
			i.State = "done"
			i.Props["result"] = d
		}
	case "validation.basis":
		bases, _ := i.Props["bases"].(map[string]any)
		if bases == nil {
			bases = map[string]any{}
		}
		bases[str(d, "basis_id")] = map[string]any(d)
		i.Props["bases"] = bases
		i.Props["current_basis"] = str(d, "basis_id")
	case "validation.record", "validation.accept":
		records, _ := i.Props["records"].([]any)
		records = append(records, map[string]any(d))
		i.Props["records"] = records
	case "validation.waive":
		i.Props["waiver"] = map[string]any(d)
	case "validation.unwaive":
		delete(i.Props, "waiver")
	}
	if i != nil {
		i.Updated = e.At
	}
	s.Revision = e.Sequence
	s.Events = append(s.Events, e)
}
func (s *State) View(i *Item) Object {
	b, _ := json.Marshal(i)
	v := Object{}
	_ = json.Unmarshal(b, &v)
	delete(v, "properties")
	for k, x := range i.Props {
		if k != "bases" && k != "records" && k != "spec" && k != "plan" {
			v[k] = x
		}
	}
	if i.Kind == "task" {
		for _, k := range []string{"acceptance", "acceptance_keys"} {
			if _, ok := v[k]; !ok {
				v[k] = []string{}
			}
		}
		r := s.Current(i.ID)
		v["running"] = r != nil
		v["current_run"] = r
		v["blockers"] = s.Blockers(i)
		v["ready"] = i.State == "open" && r == nil && len(s.Blockers(i)) == 0
		if i.Workstream == "" {
			v["workstream_id"] = nil
		}
	}
	if i.Kind == "workstream" {
		c := Object{"open": 0, "done": 0, "canceled": 0}
		for _, t := range s.Items {
			if t.Kind == "task" && t.Workstream == i.ID {
				c[t.State] = num(c, t.State) + 1
			}
		}
		v["counts"] = c
		v["blockers"] = s.Blockers(i)
	}
	return v
}
func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
