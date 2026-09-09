package tasks

import (
	"encoding/json"
	"errors"

	"github.com/jinyongp/devtools/internal/protocol"
)

const JournalVersion = 2
const ProjectionVersion = 2

// DefinitionBasis is internal projection data. It is never flattened into View.
// Epochs are independent of receipt, checkpoint, and evidence record revisions.
type DefinitionBasis struct {
	Epoch       int               `json:"epoch"`
	Removed     bool              `json:"removed"`
	Activated   bool              `json:"activated"`
	BodyEpochs  map[string]int    `json:"body_epochs"`
	KeyEpochs   map[string]int    `json:"key_epochs"`
	RemovedKeys map[string]Object `json:"removed_keys"`
	Order       []string          `json:"order"`
	Completions []CompletionBasis `json:"completions"`
	CloseEpoch  int               `json:"close_epoch"`
	LegacyBases map[string]string `json:"legacy_bases"`
	LegacyRuns  map[string]string `json:"legacy_runs"`
}

type CompletionBasis struct {
	EventID   string `json:"event_id"`
	Revision  int    `json:"revision"`
	Signature string `json:"signature"`
	Result    Object `json:"result"`
}

func newDefinitionBasis(epoch int) *DefinitionBasis {
	return &DefinitionBasis{Epoch: epoch, BodyEpochs: map[string]int{}, KeyEpochs: map[string]int{},
		RemovedKeys: map[string]Object{}, Order: []string{}, Completions: []CompletionBasis{}, LegacyBases: map[string]string{}, LegacyRuns: map[string]string{}}
}

func (s *State) definition(id string) *DefinitionBasis {
	if b := s.Tracking[id]; b != nil {
		return b
	}
	return newDefinitionBasis(0)
}

// upgradeEvent snapshots only derived baseline data, not another copy of history.
// The caller appends it in the same atomic write as the first real mutation.
func (s *State) upgradeEvent() Event {
	baseline := map[string]*DefinitionBasis{}
	for _, i := range s.List("") {
		b := newDefinitionBasis(i.Revision)
		if i.Kind == "workstream" {
			b.BodyEpochs["spec"] = num(i.Props, "spec_revision")
			b.BodyEpochs["plan"] = num(i.Props, "plan_revision")
			for _, t := range s.List("task") {
				if t.Workstream == i.ID {
					b.Order = append(b.Order, t.ID)
				}
			}
		}
		if i.Kind == "validation" {
			if bases, ok := i.Props["bases"].(map[string]any); ok {
				for id := range bases {
					if old := s.basis(i, id); old != nil && str(old, "fingerprint") == s.basisFingerprint(i) {
						b.LegacyBases[id] = str(old, "fingerprint")
					}
				}
			}
		}
		baseline[i.ID] = b
	}
	for _, event := range s.Events {
		b := baseline[event.Target]
		if b == nil {
			continue
		}
		if event.Action == "workstream.activate" {
			b.Activated = true
		}
		if event.Action == "task.completed" || event.Action == "workstream.close" {
			b.Completions = append(b.Completions, CompletionBasis{EventID: event.ID, Revision: event.Sequence, Result: event.Data})
		}
	}
	return Event{Action: "profile.upgraded", Data: Object{"version": JournalVersion, "baseline": baseline}}
}

func (s *State) applyUpgrade(e Event) {
	if s.Version != 1 || e.Target != "" || num(e.Data, "version") != JournalVersion {
		panic("invalid profile upgrade")
	}
	raw, err := json.Marshal(e.Data["baseline"])
	if err != nil {
		panic(err)
	}
	var baseline map[string]*DefinitionBasis
	if json.Unmarshal(raw, &baseline) != nil || len(baseline) != len(s.Items) {
		panic("invalid baseline")
	}
	for id := range s.Items {
		b := baseline[id]
		if b == nil || b.Epoch < 0 || b.BodyEpochs == nil || b.KeyEpochs == nil || b.RemovedKeys == nil || b.LegacyBases == nil {
			panic("missing definition baseline")
		}
	}
	s.Tracking = baseline
	s.Version = JournalVersion
	s.migrating = true
	assessments := s.Assessments()
	for id, b := range baseline {
		v := s.Items[id]
		if v.Kind != "validation" {
			continue
		}
		owner := s.owner(v)
		for basisID := range b.LegacyBases {
			b.LegacyBases[basisID] = s.fingerprintFor(v, assessments[owner.ID].Signature)
			if r := s.Current(owner.ID); r != nil {
				b.LegacyRuns[basisID] = r.ID
			}
		}
	}
	for id, b := range baseline {
		if len(b.Completions) > 0 {
			last := &b.Completions[len(b.Completions)-1]
			last.Signature = assessments[id].Signature
			for _, v := range s.List("validation") {
				if o := s.owner(v); o != nil && o.ID == id && v.Props["required"] != false {
					if bID := str(v.Props, "current_basis"); s.definition(v.ID).LegacyBases[bID] == "" {
						last.Signature = "legacy-stale"
					}
				}
			}
		}
	}
	for _, run := range s.Runs {
		if run.State == "running" {
			run.Signature = assessments[run.TaskID].Signature
		}
	}
	s.migrating = false
	s.assessments = nil
}

func (s *State) trackEvent(e Event, before string, beforeSpec, beforePlan Object) {
	i := s.Items[e.Target]
	if i == nil {
		return
	}
	b := s.Tracking[i.ID]
	if b == nil {
		b = newDefinitionBasis(e.Sequence)
		s.Tracking[i.ID] = b
	}
	if before != hash(s.ownDefinition(i)) {
		b.Epoch = e.Sequence
	}
	if e.Action == "spec.set" {
		spec := objectValue(i.Props["spec"])
		if hash(beforeSpec["body"]) != hash(spec["body"]) {
			b.BodyEpochs["spec"] = e.Sequence
		}
		for _, field := range []string{"requirements", "acceptance"} {
			prefix := "acceptance:"
			if field == "requirements" {
				prefix = "requirement:"
			}
			for _, v := range append(objects(beforeSpec, field), objects(spec, field)...) {
				key := str(v, "key")
				if hash(keyed(objects(beforeSpec, field), key)) != hash(keyed(objects(spec, field), key)) {
					b.KeyEpochs[prefix+key] = e.Sequence
				}
			}
		}
	}
	if e.Action == "plan.set" && hash(beforePlan["body"]) != hash(objectValue(i.Props["plan"])["body"]) {
		b.BodyEpochs["plan"] = e.Sequence
	}
	if e.Action == "workstream.activate" {
		b.Activated = true
	}
	if e.Action == "task.add" && i.Workstream != "" {
		if owner := s.Tracking[i.Workstream]; owner != nil && !contains(owner.Order, i.ID) {
			owner.Order = append(owner.Order, i.ID)
		}
	}
	if e.Action == "run.claimed" || e.Action == "run.taken_over" {
		if run := s.Current(i.ID); run != nil {
			run.Signature = s.Assessment(i.ID).Signature
		}
	}
	if e.Action == "task.completed" || e.Action == "workstream.close" {
		b.Completions = append(b.Completions, CompletionBasis{EventID: e.ID, Revision: e.Sequence,
			Signature: str(e.Data, "definition_signature"), Result: e.Data})
		if b.Completions[len(b.Completions)-1].Signature == "" {
			b.Completions[len(b.Completions)-1].Signature = s.Assessment(i.ID).Signature
		}
	}
	s.assessments = nil
}

func replayJournal(j *Journal) (*State, *protocol.Error) {
	if j.Version != 1 && j.Version != JournalVersion {
		return nil, protocol.NewError("unsupported_storage_version", "Upgrade devtools to read this task journal.", 3,
			map[string]any{"version": j.Version, "supported_versions": []int{1, JournalVersion}})
	}
	s := NewState()
	for n, e := range j.Events {
		if e.Sequence != n+1 || safeApply(s, e) != nil {
			return nil, storageError()
		}
	}
	if s.Version != j.Version {
		return nil, storageError()
	}
	return s, nil
}

// AtRevision exposes only complete requests, even when one write appended
// upgrade + mutation or reopen + claim events.
func (s *State) AtRevision(revision int) (*State, *protocol.Error) {
	if revision <= 0 || revision > s.Revision {
		return nil, failure("invalid_argument", "Choose an existing positive profile revision.")
	}
	if revision < len(s.Events) && s.Events[revision-1].RequestID == s.Events[revision].RequestID {
		return nil, failure("revision_not_committed", "Choose the final revision of this atomic request.")
	}
	out := NewState()
	for _, e := range s.Events[:revision] {
		if safeApply(out, e) != nil {
			return nil, storageError()
		}
	}
	return out, nil
}

func (s *State) clone() *State {
	raw, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	out := NewState()
	if json.Unmarshal(raw, out) != nil {
		panic(errors.New("cannot clone task state"))
	}
	for id, i := range s.Items {
		out.Items[id].Order = i.Order
	}
	return out
}
