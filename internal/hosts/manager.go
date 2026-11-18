package hosts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Owner struct {
	Key      string    `json:"key"`
	Mappings []Mapping `json:"mappings"`
}
type state struct {
	Owners  []Owner  `json:"owners"`
	Pending *pending `json:"pending,omitempty"`
}
type pending struct {
	Owners   []Owner    `json:"owners"`
	Request  Request    `json:"request"`
	After    string     `json:"after"`
	Step     *stepInput `json:"step,omitempty"`
	Previous []Owner    `json:"previous,omitempty"`
}
type Manager struct {
	Data     string
	Isolated bool
	Terminal bool
	System   System
	Run      privilege.Runner
	Preview  func([]Mapping)
	intent   intent
}

type intent struct {
	RequestID string           `json:"request_id"`
	Operation string           `json:"operation"`
	Route     network.RouteRef `json:"route"`
	Listener  string           `json:"listener_id,omitempty"`
}

type stepInput struct {
	Intent   intent    `json:"intent"`
	ID       string    `json:"id"`
	Owner    string    `json:"owner"`
	Desired  []Mapping `json:"desired"`
	Expected string    `json:"preview_digest,omitempty"`
}

// ForRequest scopes subsequent mutations to one frozen operation and phase.
func (m Manager) ForRequest(id, operation string, route network.RouteRef, listener string) Manager {
	m.intent = intent{strings.ToLower(id), operation, route, listener}
	return m
}

func stepID(input any) string {
	data, _ := json.Marshal(input)
	hash := digest(data)[:32]
	return hash[:8] + "-" + hash[8:12] + "-" + hash[12:16] + "-" + hash[16:20] + "-" + hash[20:]
}

func (m Manager) Status() (map[string]any, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	count := 0
	prepared := 0
	observation := m.Observe()
	for _, owner := range st.Owners {
		count += len(owner.Mappings)
		for _, mapping := range owner.Mappings {
			if observation.MappingIssue(owner.Key, mapping) == "" {
				prepared++
			}
		}
	}
	return map[string]any{"owners": len(st.Owners), "mappings": count, "prepared": prepared, "pending": st.Pending != nil}, nil
}
func (m Manager) ReferenceDigest() (string, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return "", e
	}
	return stateDigest(st), nil
}

func (m Manager) read() (state, *protocol.Error) {
	var st state
	e := tasks.ReadPrivate(filepath.Join(m.Data, "hosts", "references.json"), &st)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return st, failure("invalid_storage")
	}
	return st, nil
}
func mappings(owners []Owner) []Mapping {
	out := []Mapping{}
	for _, o := range owners {
		out = append(out, o.Mappings...)
	}
	return out
}
func (m Manager) Prepared(owner string, mapping Mapping) bool {
	return m.Observe().MappingIssue(owner, mapping) == ""
}
func (m Manager) OwnerMappings(owner string) ([]Mapping, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	for _, o := range st.Owners {
		if o.Key == owner {
			return append([]Mapping{}, o.Mappings...), nil
		}
	}
	return nil, nil
}
func (m Manager) RemoveOtherOwners(ctx context.Context, prefix, keep string) *protocol.Error {
	return m.removeOwners(ctx, prefix, keep)
}
func (m Manager) RemovePrefix(ctx context.Context, prefix string) *protocol.Error {
	return m.removeOwners(ctx, prefix, "")
}
func (m Manager) removeOwners(ctx context.Context, prefix, keep string) *protocol.Error {
	selectOwners := func() ([]string, *protocol.Error) {
		st, e := m.read()
		if e != nil {
			return nil, e
		}
		owners := append([]Owner{}, st.Owners...)
		if st.Pending != nil {
			owners = append(owners, st.Pending.Owners...)
		}
		selected := map[string]bool{}
		for _, owner := range owners {
			if owner.Key != keep && strings.HasPrefix(owner.Key, prefix) {
				selected[owner.Key] = true
			}
		}
		keys := make([]string, 0, len(selected))
		for key := range selected {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys, nil
	}
	apply := func(keys []string) *protocol.Error {
		for _, key := range keys {
			if e := m.Sync(ctx, key, nil, ""); e != nil {
				return e
			}
		}
		return nil
	}
	if m.intent.RequestID == "" {
		keys, e := selectOwners()
		if e != nil {
			return e
		}
		return apply(keys)
	}
	input := struct {
		Intent       intent
		Prefix, Keep string
	}{m.intent, prefix, keep}
	_, e := (netops.Store{Data: m.Data}).Apply(ctx, stepID(input), input, nil, func(j *netops.Journal) (any, *protocol.Error) {
		var keys []string
		found, e := j.Get("owners", &keys)
		if e != nil {
			return nil, e
		}
		if !found {
			keys, e = selectOwners()
			if e != nil {
				return nil, e
			}
			if e := j.Set("owners", keys); e != nil {
				return nil, e
			}
		}
		return map[string]any{"owners": keys}, apply(keys)
	})
	return e
}
func (m Manager) PreviewChange(owner string, desired []Mapping) (map[string]any, *protocol.Error) {
	before, e := m.System.Read()
	if e != nil {
		return nil, e
	}
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	if st.Pending != nil {
		return nil, failure("recovery_required")
	}
	if e := validateOwnership(before, mappings(st.Owners)); e != nil {
		return nil, e
	}
	owners := replaceOwner(st.Owners, owner, desired)
	after, e := Render(before, mappings(owners))
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(before, after) {
		if e := privilege.CheckRequest("hosts", Request{Expected: digest(before), Mappings: mappings(owners)}); e != nil {
			return nil, e
		}
	}
	return map[string]any{"digest": digest(before), "changed": !bytes.Equal(before, after), "mappings": desired}, nil
}
func replaceOwner(owners []Owner, key string, desired []Mapping) []Owner {
	out := []Owner{}
	for _, o := range owners {
		if o.Key != key {
			out = append(out, o)
		}
	}
	if len(desired) > 0 {
		out = append(out, Owner{key, desired})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (m Manager) Sync(ctx context.Context, owner string, desired []Mapping, expected string) *protocol.Error {
	_, e := m.Change(ctx, owner, desired, expected)
	return e
}

// Change resumes only the child phase named by its caller. An unscoped call
// gets a fresh step, so it cannot inherit work from another operation.
func (m Manager) Change(ctx context.Context, owner string, desired []Mapping, expected string) (map[string]any, *protocol.Error) {
	if owner == "" {
		return nil, failure("invalid_argument")
	}
	if m.Isolated && len(desired) > 0 {
		return nil, isolatedWriteFailure()
	}
	step := stepInput{Intent: m.intent, Owner: owner, Desired: desired, Expected: expected}
	if m.intent.RequestID == "" {
		step.ID = protocol.NewRequestID()
	} else {
		step.ID = stepID(struct {
			Intent intent
			Owner  string
		}{m.intent, owner})
	}
	result, e := (netops.Store{Data: m.Data}).Apply(ctx, step.ID, step, nil, func(j *netops.Journal) (any, *protocol.Error) {
		return m.changeLocked(ctx, step, j)
	})
	if e != nil {
		return nil, e
	}
	return result.(map[string]any), nil
}

func (m Manager) lock(ctx context.Context) (string, func(), *protocol.Error) {
	root := filepath.Join(m.Data, "hosts")
	if tasks.PrivateDir(root) != nil {
		return "", nil, failure("io_error")
	}
	unlock, err := tasks.Lock(ctx, filepath.Join(root, "references.lock"))
	if err != nil {
		return "", nil, failure("canceled")
	}
	return root, unlock, nil
}

func (m Manager) changeLocked(ctx context.Context, step stepInput, j *netops.Journal) (map[string]any, *protocol.Error) {
	root, unlock, e := m.lock(ctx)
	if e != nil {
		return nil, e
	}
	defer unlock()
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	var verified map[string]any
	found, e := j.Get("verified", &verified)
	if e != nil {
		return nil, e
	}
	if found && st.Pending == nil {
		var published string
		present, e := j.Get("published", &published)
		if e != nil {
			return nil, e
		}
		if present && published == referenceStamp(root) && verified["references_digest"] == stateDigest(st) {
			return verified, nil
		}
		return nil, recoveryFailure("outcome_unconfirmed", st.Pending)
	}
	before, busy, e := m.System.observe()
	if e != nil {
		return nil, e
	}
	if busy {
		return nil, recoveryFailure("outcome_unconfirmed", st.Pending)
	}
	owners := replaceOwner(st.Owners, step.Owner, step.Desired)
	if p := st.Pending; p != nil {
		if p.Step != nil && p.Step.ID == step.ID && reflect.DeepEqual(*p.Step, step) {
			return m.finishPending(ctx, root, st, before, j)
		}
		if reflect.DeepEqual(owners, st.Owners) || len(owners) == 0 && len(st.Owners) == 0 {
			if step.Expected == "" || step.Expected == digest(before) {
				if !sameOwner(st.Owners, p.Owners, step.Owner) {
					return nil, recoveryFailure("foreign_pending", p)
				}
				after, renderErr := Render(before, mappings(owners))
				if renderErr == nil && bytes.Equal(before, after) && validateOwnership(before, mappings(owners)) == nil {
					return stepResult(step.ID, false, "not_performed", st), nil
				}
			}
		}
		return nil, recoveryFailure("foreign_pending", p)
	}
	if found {
		return nil, recoveryFailure("outcome_unconfirmed", nil)
	}
	if step.Expected != "" && step.Expected != digest(before) {
		return nil, failure("stale_preview")
	}
	if e := validateOwnership(before, mappings(st.Owners)); e != nil {
		return nil, e
	}
	after, e := Render(before, mappings(owners))
	if e != nil {
		return nil, e
	}
	if bytes.Equal(before, after) {
		changed := !reflect.DeepEqual(st.Owners, owners) && (len(st.Owners) != 0 || len(owners) != 0)
		if !changed {
			return stepResult(step.ID, false, "not_performed", st), nil
		}
		st.Owners = owners
		result := stepResult(step.ID, true, "not_performed", st)
		if e := j.Set("verified", result); e != nil {
			return nil, e
		}
		return result, publishState(root, st, j)
	}
	if m.Isolated {
		return nil, isolatedWriteFailure()
	}
	q := Request{Expected: digest(before), Mappings: mappings(owners)}
	if e := privilege.CheckRequest("hosts", q); e != nil {
		return nil, e
	}
	st.Pending = &pending{Owners: owners, Request: q, After: digest(after), Step: &step, Previous: st.Owners}
	if e := j.Set("plan", st.Pending); e != nil {
		return nil, e
	}
	if e := writeState(root, st); e != nil {
		return nil, e
	}
	return m.finishPending(ctx, root, st, before, j)
}

func sameOwner(a, b []Owner, key string) bool {
	find := func(owners []Owner) []Mapping {
		for _, owner := range owners {
			if owner.Key == key {
				return owner.Mappings
			}
		}
		return nil
	}
	return reflect.DeepEqual(find(a), find(b))
}

func validPending(st state) bool {
	p := st.Pending
	if p == nil || !reflect.DeepEqual(p.Request.Mappings, mappings(p.Owners)) {
		return false
	}
	return p.Step == nil || (reflect.DeepEqual(p.Previous, st.Owners) || len(p.Previous) == 0 && len(st.Owners) == 0) &&
		reflect.DeepEqual(p.Owners, replaceOwner(st.Owners, p.Step.Owner, p.Step.Desired))
}

func stepResult(id string, changed bool, write string, st state) map[string]any {
	return map[string]any{"step_id": id, "changed": changed, "os_write": write, "references_digest": stateDigest(st)}
}

func isolatedWriteFailure() *protocol.Error {
	return netops.Failure("isolated_operation_not_allowed", "System hosts changes are unavailable in isolated mode.")
}

func recoveryFailure(reason string, p *pending) *protocol.Error {
	facts := map[string]any{"state": reason}
	if p != nil && p.Step != nil {
		facts["origin_request_id"] = p.Step.Intent.RequestID
		facts["step_id"] = p.Step.ID
	}
	problem := netops.Failure("recovery_required", "Inspect the pending hosts operation with devtools proxy hosts status.")
	problem.Details = map[string]any{"facts": map[string]any{"hosts": facts}}
	return problem
}

func (m Manager) finishPending(ctx context.Context, root string, st state, before []byte, j *netops.Journal) (map[string]any, *protocol.Error) {
	p := st.Pending
	if !validPending(st) {
		return nil, recoveryFailure("ownership_unconfirmed", p)
	}
	switch digest(before) {
	case p.After:
		if validateOwnership(before, mappings(p.Owners)) != nil {
			return nil, recoveryFailure("ownership_unconfirmed", p)
		}
	case p.Request.Expected:
		if validateOwnership(before, mappings(st.Owners)) != nil {
			return nil, recoveryFailure("ownership_unconfirmed", p)
		}
		after, e := Render(before, p.Request.Mappings)
		if e != nil || digest(after) != p.After {
			return nil, recoveryFailure("ownership_unconfirmed", p)
		}
		if m.Isolated {
			return nil, isolatedWriteFailure()
		}
		if e := privilege.CheckRequest("hosts", p.Request); e != nil {
			return nil, e
		}
		if e := m.applyPending(ctx, p); e != nil {
			return nil, e
		}
	default:
		return nil, recoveryFailure("file_diverged", p)
	}
	next := state{Owners: p.Owners}
	id := ""
	if p.Step != nil {
		id = p.Step.ID
	}
	result := stepResult(id, true, "performed", next)
	if e := j.Set("verified", result); e != nil {
		return nil, e
	}
	return result, publishState(root, next, j)
}

func publishState(root string, st state, j *netops.Journal) *protocol.Error {
	if e := writeState(root, st); e != nil {
		return e
	}
	stamp := referenceStamp(root)
	if stamp == "" {
		return recoveryFailure("outcome_unconfirmed", nil)
	}
	return j.Set("published", stamp)
}

func (m Manager) applyPending(ctx context.Context, p *pending) *protocol.Error {
	if m.Preview != nil {
		m.Preview(p.Request.Mappings)
	}
	problem := m.apply(ctx, p.Request)
	current, busy, e := m.System.observe()
	if e != nil || busy {
		return recoveryFailure("outcome_unconfirmed", p)
	}
	currentDigest := digest(current)
	if problem != nil && (currentDigest == p.Request.Expected || currentDigest == p.After) {
		return problem
	}
	if currentDigest != p.After {
		return recoveryFailure("outcome_unconfirmed", p)
	}
	return nil
}
func writeState(root string, st state) *protocol.Error {
	if tasks.WritePrivate(filepath.Join(root, "references.json"), st) != nil {
		return failure("io_error")
	}
	return nil
}
func (m Manager) apply(ctx context.Context, q Request) *protocol.Error {
	run := m.Run
	if run == nil {
		run = privilege.Run
	}
	return run(ctx, "hosts", q, m.Terminal)
}
