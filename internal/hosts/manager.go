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
	"github.com/jinyongp/devtools/internal/platformfs"
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
	Owners   []Owner           `json:"owners"`
	Request  Request           `json:"request"`
	After    string            `json:"after"`
	Step     *stepInput        `json:"step,omitempty"`
	Previous []Owner           `json:"previous,omitempty"`
	Approval *recoveryApproval `json:"approval,omitempty"`
}
type Manager struct {
	Data     string
	Isolated bool
	Terminal bool
	System   System
	Run      privilege.Runner
	Preview  func([]Mapping)
	intent   intent
	guard    *Publication
	origin   *string
}

type intent struct {
	RequestID string           `json:"request_id"`
	OriginID  *string          `json:"origin_request_id,omitempty"`
	Operation string           `json:"operation"`
	Route     network.RouteRef `json:"route"`
	Listener  string           `json:"listener_id,omitempty"`
}

type stepInput struct {
	Intent   intent       `json:"intent"`
	ID       string       `json:"id"`
	Owner    string       `json:"owner"`
	Desired  []Mapping    `json:"desired"`
	Expected string       `json:"preview_digest,omitempty"`
	Guard    *Publication `json:"publication_guard,omitempty"`
}

// Publication is private evidence that a hosts step replaced the journal.
type Publication struct {
	Stamp  string
	Digest string
}

func Published(result map[string]any) *Publication {
	stamp, _ := result["publication_stamp"].(string)
	hash, _ := result["references_digest"].(string)
	if stamp == "" || hash == "" {
		return nil
	}
	return &Publication{stamp, hash}
}

func (m Manager) IfPublished(publication *Publication) Manager {
	m.guard = publication
	return m
}

// ForRequest scopes subsequent mutations to one frozen operation and phase.
func (m Manager) ForRequest(id, operation string, route network.RouteRef, listener string) Manager {
	m.intent = intent{RequestID: strings.ToLower(id), OriginID: m.origin, Operation: operation, Route: route, Listener: listener}
	return m
}

// WithOrigin binds child phases to the initiating public request. An empty
// origin records that the invocation did not expose an operation UUID.
func (m Manager) WithOrigin(id string) Manager {
	id = strings.ToLower(id)
	m.origin = &id
	return m
}

func (i intent) publicOrigin() string {
	if i.OriginID != nil {
		return *i.OriginID
	}
	return i.RequestID
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
	_, e := m.RemoveOtherOwnersResult(ctx, prefix, keep)
	return e
}
func (m Manager) RemoveOtherOwnersResult(ctx context.Context, prefix, keep string) (map[string]any, *protocol.Error) {
	return m.removeOwners(ctx, prefix, keep)
}
func (m Manager) RemovePrefix(ctx context.Context, prefix string) *protocol.Error {
	_, e := m.RemovePrefixResult(ctx, prefix)
	return e
}
func (m Manager) RemovePrefixResult(ctx context.Context, prefix string) (map[string]any, *protocol.Error) {
	return m.removeOwners(ctx, prefix, "")
}
func (m Manager) removeOwners(ctx context.Context, prefix, keep string) (map[string]any, *protocol.Error) {
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
	apply := func(keys []string) (map[string]any, *protocol.Error) {
		changed, write := false, "not_performed"
		for _, key := range keys {
			result, e := m.Change(ctx, key, nil, "")
			if e != nil {
				return nil, e
			}
			changed = changed || result["changed"] == true
			if result["os_write"] == "performed" {
				write = "performed"
			}
		}
		return map[string]any{"changed": changed, "os_write": write}, nil
	}
	if m.intent.RequestID == "" {
		keys, e := selectOwners()
		if e != nil {
			return nil, e
		}
		return apply(keys)
	}
	input := struct {
		Intent       intent
		Prefix, Keep string
	}{m.intent, prefix, keep}
	result, e := (netops.Store{Data: m.Data}).Apply(ctx, stepID(input), input, nil, func(j *netops.Journal) (any, *protocol.Error) {
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
		return apply(keys)
	})
	if e != nil {
		return nil, e
	}
	return result.(map[string]any), nil
}
func (m Manager) PreviewChange(owner string, desired []Mapping) (map[string]any, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	if st.Pending != nil {
		return nil, recoveryFailure("pending_hosts_operation", st.Pending)
	}
	before, busy, e := m.System.observe()
	if e != nil {
		return nil, e
	}
	if busy {
		return nil, recoveryFailure("outcome_unconfirmed", nil)
	}
	current, e := m.read()
	if e != nil {
		return nil, e
	}
	if stateDigest(current) != stateDigest(st) {
		return nil, staleRecovery()
	}
	if e := validateOwnership(before, mappings(st.Owners)); e != nil {
		return nil, e
	}
	owners := replaceOwner(st.Owners, owner, desired)
	after, e := Render(before, mappings(owners))
	if e != nil {
		return nil, e
	}
	osChanged := !bytes.Equal(before, after)
	if osChanged {
		if e := privilege.CheckRequest("hosts", Request{Expected: digest(before), Mappings: mappings(owners)}); e != nil {
			return nil, e
		}
	}
	effect := ""
	if osChanged {
		effect = "system_write"
	} else if !sameOwner(st.Owners, owners, owner) {
		effect = "value_write"
	}
	if m.Isolated && (osChanged || len(desired) > 0) {
		effect = ""
	}
	if desired == nil {
		desired = []Mapping{}
	}
	return map[string]any{"digest": m.previewToken(st, before, owner, desired), "changed": osChanged, "mappings": desired, "effect": effect}, nil
}

func (m Manager) previewToken(st state, before []byte, owner string, desired []Mapping) string {
	return m.token("hosts-preview", stateDigest(st), digest(before), owner, append([]Mapping{}, desired...))
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
	step := stepInput{Intent: m.intent, Owner: owner, Desired: desired, Expected: expected, Guard: m.guard}
	if step.Intent.Listener == "" {
		if i := strings.LastIndexByte(owner, '/'); i >= 0 && protocol.ValidUUID(owner[i+1:]) {
			if route := ownerRoute(owner[:i]); route.Valid() && (!step.Intent.Route.Valid() || route == step.Intent.Route) {
				step.Intent.Route, step.Intent.Listener = route, owner[i+1:]
			}
		}
	}
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

func ownerRoute(key string) network.RouteRef {
	parts := strings.Split(key, "/")
	if len(parts) == 2 && parts[0] == "global" {
		return network.RouteRef{Scope: "global", RouteID: parts[1]}
	}
	if len(parts) == 4 && parts[0] == "project" {
		return network.RouteRef{Scope: "project", Profile: parts[1], InstanceID: parts[2], Proxy: parts[3]}
	}
	return network.RouteRef{}
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
			verified["publication_stamp"] = published
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
		if sameStep(p.Step, &step) {
			if p.Approval != nil {
				return nil, recoveryFailure("pending_hosts_operation", p)
			}
			return m.finishPending(ctx, root, st, before, j)
		}
		if sameOwner(st.Owners, owners, step.Owner) {
			if step.Expected == "" || step.Expected == m.previewToken(st, before, step.Owner, step.Desired) {
				if !sameOwner(st.Owners, p.Owners, step.Owner) {
					return nil, recoveryFailure("pending_hosts_operation", p)
				}
				if confirmedNoop(st, before, step) {
					return stepResult(step.ID, false, "not_performed", st), nil
				}
			}
		}
		problem := recoveryFailure("pending_hosts_operation", p)
		problem.Details["facts"].(map[string]any)["hosts"].(map[string]any)["file_state"] = m.recovery(st, before, false).Status.State
		return nil, problem
	}
	if found {
		return nil, recoveryFailure("outcome_unconfirmed", nil)
	}
	if step.Guard != nil && (step.Guard.Stamp != referenceStamp(root) || step.Guard.Digest != stateDigest(st)) {
		return nil, recoveryFailure("outcome_unconfirmed", nil)
	}
	if step.Expected != "" && step.Expected != m.previewToken(st, before, step.Owner, step.Desired) {
		return nil, failure("stale_preview")
	}
	if step.Expected == "" && sameOwner(st.Owners, owners, step.Owner) && confirmedNoop(st, before, step) {
		return stepResult(step.ID, false, "not_performed", st), nil
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
		return result, publishState(root, st, j, result)
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

func confirmedNoop(st state, body []byte, step stepInput) bool {
	o := observe(st, body, false)
	if o.issue != "" {
		return false
	}
	for _, mapping := range step.Desired {
		if o.MappingIssue(step.Owner, mapping) != "" {
			return false
		}
	}
	return true
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
	if p.Step == nil {
		owner := ""
		for _, o := range append(append([]Owner{}, st.Owners...), p.Owners...) {
			if sameOwner(st.Owners, p.Owners, o.Key) {
				continue
			}
			if owner != "" && owner != o.Key {
				return false
			}
			owner = o.Key
		}
		return owner != ""
	}
	return (reflect.DeepEqual(p.Previous, st.Owners) || len(p.Previous) == 0 && len(st.Owners) == 0) &&
		reflect.DeepEqual(p.Owners, replaceOwner(st.Owners, p.Step.Owner, p.Step.Desired))
}

func stepResult(id string, changed bool, write string, st state) map[string]any {
	return map[string]any{"step_id": id, "changed": changed, "os_write": write, "references_digest": stateDigest(st)}
}

func isolatedWriteFailure() *protocol.Error {
	return netops.Failure("isolated_operation_not_allowed", "System hosts changes are unavailable in isolated mode.")
}

func recoveryFailure(reason string, p *pending) *protocol.Error {
	facts := map[string]any{}
	if p != nil && p.Step != nil {
		facts["origin_request_id"] = p.Step.Intent.publicOrigin()
		facts["step_id"] = p.Step.ID
		facts["listener_id"] = p.Step.Intent.Listener
		if p.Step.Intent.Route.Valid() {
			facts["route_ref"] = p.Step.Intent.Route
		}
	}
	effects := "none"
	if reason == "outcome_unconfirmed" || reason == "file_diverged" {
		effects = "unknown"
		reason = "outcome_unconfirmed"
	}
	problem := netops.Failure("recovery_required", "Inspect the pending hosts operation with devtools proxy hosts status.")
	if reason == "pending_hosts_operation" {
		problem.Message = "A different hosts operation is pending. Review its recorded mappings before changing hosts."
	}
	problem.Details = map[string]any{"reason": reason, "retry": "inspect_first", "effects": effects, "facts": map[string]any{"hosts": facts}}
	problem.Details["remedies"] = []protocol.Remedy{StatusAction().Remedy(protocol.InvocationContext{})}
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
	return result, publishState(root, next, j, result)
}

func publishState(root string, st state, j *netops.Journal, result map[string]any) *protocol.Error {
	if e := writeState(root, st); e != nil {
		return e
	}
	stamp := referenceStamp(root)
	if stamp == "" {
		return recoveryFailure("outcome_unconfirmed", nil)
	}
	result["publication_stamp"] = stamp
	return j.Set("published", stamp)
}

func (m Manager) applyPending(ctx context.Context, p *pending) *protocol.Error {
	if m.Preview != nil {
		m.Preview(p.Request.Mappings)
	}
	problem := m.apply(ctx, p.Request)
	current, busy, e := m.System.observe()
	if ctx.Err() != nil {
		canceled := failure("canceled")
		observed := recoveryFailure("outcome_unconfirmed", p)
		canceled.Details = observed.Details
		if e == nil && !busy && digest(current) == p.Request.Expected {
			canceled.Details["effects"] = "none"
		}
		return canceled
	}
	if e != nil || busy {
		return recoveryFailure("outcome_unconfirmed", p)
	}
	currentDigest := digest(current)
	if problem != nil && currentDigest == p.Request.Expected {
		if problem.Details == nil {
			problem.Details = map[string]any{}
		}
		problem.Details["remedies"] = []protocol.Remedy{StatusAction().Remedy(protocol.InvocationContext{})}
		return problem
	}
	if problem != nil && currentDigest == p.After {
		return recoveryFailure("outcome_unconfirmed", p)
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

func referenceStamp(root string) string {
	stamp, _ := platformfs.PrivateStamp(filepath.Join(root, "references.json"))
	return stamp
}
func (m Manager) apply(ctx context.Context, q Request) *protocol.Error {
	run := m.Run
	if run == nil {
		run = privilege.Run
	}
	return run(ctx, "hosts", q, m.Terminal)
}
