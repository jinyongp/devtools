package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Receipt struct {
	Fingerprint string `json:"fingerprint"`
	ContextHash string `json:"context_hash"`
	Result      Object `json:"result"`
}
type Journal struct {
	Version  int                `json:"version"`
	Profile  string             `json:"profile"`
	Events   []Event            `json:"events"`
	Receipts map[string]Receipt `json:"receipts"`
	Contexts map[string]string  `json:"contexts"`
}
type Store struct {
	Directory string
	Profile   string
	Cache     string
	// Test seam around the active-v3 WAL fsync commit point.
	commitV3 func(func() error) error
	// Test seam between a lockless completion read and its stability recheck.
	completionHook func()
}

func (s Store) path() string {
	return profilekey.CanonicalPath(s.Directory, s.Profile)
}
func storageError() *protocol.Error {
	return protocol.NewError("storage_error", "Cannot access private task storage.", 1, nil)
}

func canceledError() *protocol.Error {
	return protocol.NewError("canceled", "Request canceled.", 130, nil)
}

func gateError(ctx context.Context) *protocol.Error {
	if ctx.Err() != nil {
		return canceledError()
	}
	return storageError()
}
func PrivateDir(path string) error {
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return errors.New("private directory required")
	}
	return nil
}
func ReadPrivate(path string, v any) error {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return e
	}
	if !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
		return errors.New("private file required")
	}
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("expected one stored document")
	}
	return nil
}
func WritePrivate(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".write-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	c := f.Close()
	if e != nil {
		return e
	}
	if c != nil {
		return c
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func Lock(ctx context.Context, path string) (func(), error) {
	return maintenance.LockExclusive(ctx, path)
}

func LockShared(ctx context.Context, path string) (func(), error) {
	return maintenance.LockShared(ctx, path)
}

func LockExclusive(ctx context.Context, path string) (func(), error) {
	return maintenance.LockExclusive(ctx, path)
}

func (s Store) emptyJournal() *Journal {
	return &Journal{Version: 1, Profile: s.Profile, Events: []Event{}, Receipts: map[string]Receipt{}, Contexts: map[string]string{}}
}

func (s Store) loadPath(path string) (*Journal, *State, *protocol.Error) {
	j := s.emptyJournal()
	if e := ReadPrivate(path, j); e != nil {
		return nil, nil, storageError()
	}
	if j.Profile != s.Profile || j.Receipts == nil || j.Contexts == nil {
		return nil, nil, storageError()
	}
	state, err := replayJournal(j)
	return j, state, err
}

func (s Store) loadResolved() (*Journal, *State, taskStorageInfo, *protocol.Error) {
	info, resolveErr := s.resolveStorage()
	if resolveErr != nil {
		return nil, nil, info, resolveErr
	}
	if info.V3 != nil {
		journal, state, err := s.loadV3(*info.V3)
		return journal, state, info, err
	}
	switch info.Profile.Mode {
	case profilekey.ModeNone:
		journal := s.emptyJournal()
		state, err := replayJournal(journal)
		return journal, state, info, err
	case profilekey.ModeLegacy:
		journal, state, err := s.loadPath(info.Profile.LegacyPath)
		return journal, state, info, err
	case profilekey.ModeCanonical:
		journal, state, err := s.loadPath(info.Profile.CanonicalPath)
		return journal, state, info, err
	default:
		return nil, nil, info, storageError()
	}
}

func (s Store) load() (*Journal, *State, *protocol.Error) {
	journal, state, _, err := s.loadResolved()
	return journal, state, err
}
func safeApply(s *State, e Event) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("invalid journal")
		}
	}()
	s.Apply(e)
	return nil
}
func (s Store) Read(ctx context.Context) (*State, *protocol.Error) {
	release, e := maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	if e != nil {
		return nil, gateError(ctx)
	}
	defer release()
	return s.ReadHeld(ctx)
}

// ReadHeld requires the caller to hold the data-root maintenance gate and adds
// the profile shared lock needed by correctness-bearing task reads.
func (s Store) ReadHeld(ctx context.Context) (*State, *protocol.Error) {
	if ctx.Err() != nil {
		return nil, canceledError()
	}
	release, e := LockShared(ctx, s.path()+".lock")
	if e != nil {
		return nil, gateError(ctx)
	}
	defer release()
	return s.loadHistory()
}

func (s Store) ReadCurrent(ctx context.Context) (*State, *protocol.Error) {
	release, e := maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	if e != nil {
		return nil, gateError(ctx)
	}
	defer release()
	return s.ReadCurrentHeld(ctx)
}

// ReadCurrentHeld requires the caller to hold the data-root maintenance gate.
// It validates the active physical storage under the profile shared lock but
// materializes only compact current state plus the bounded WAL tail.
func (s Store) ReadCurrentHeld(ctx context.Context) (*State, *protocol.Error) {
	if ctx.Err() != nil {
		return nil, canceledError()
	}
	release, e := LockShared(ctx, s.path()+".lock")
	if e != nil {
		return nil, gateError(ctx)
	}
	defer release()
	return s.loadCurrent()
}

// CompletionIDs reads a stable best-effort snapshot without maintenance or
// profile locks. V3 head changes retry once instead of exposing a torn view.
func (s Store) CompletionIDs(kind, workstream string) ([]string, *protocol.Error) {
	state, e := s.completionState()
	if e != nil {
		return nil, e
	}
	ids := []string{}
	if kind == "run" {
		for id := range state.Runs {
			ids = append(ids, id)
		}
		return ids, nil
	}
	for id, item := range state.Items {
		if item.Kind == kind && (workstream == "" || kind != "task" || item.Workstream == workstream) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
func (s Store) Profiles(ctx context.Context) ([]string, *protocol.Error) {
	release, gateErr := maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	if gateErr != nil {
		return nil, gateError(ctx)
	}
	defer release()
	items, err := profilekey.Enumerate(s.Directory, "tasks")
	if err != nil {
		return nil, storageError()
	}
	return items, nil
}

type Request struct {
	Action  string
	Target  string
	Body    Object
	Options map[string]string
}

func (s Store) Execute(ctx context.Context, r Request) (Object, *protocol.Error) {
	return s.execute(ctx, r, false)
}

func (s Store) execute(ctx context.Context, r Request, exclusive bool) (Object, *protocol.Error) {
	var releaseGate func()
	var gateErr error
	if exclusive {
		releaseGate, gateErr = maintenance.AcquireExclusive(ctx, maintenance.Root(s.Directory))
	} else {
		releaseGate, gateErr = maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	}
	if gateErr != nil {
		return nil, gateError(ctx)
	}
	defer func() {
		if releaseGate != nil {
			releaseGate()
		}
	}()
	def := Find(r.Action)
	if def == nil {
		return nil, failure("invalid_argument", "Unknown task action.")
	}
	if def.Target && !validID(r.Target) {
		return nil, failure("invalid_argument", "Provide a target UUID.")
	}
	if r.Target != "" && !validID(r.Target) {
		return nil, failure("invalid_argument", "Invalid target UUID.")
	}
	if (r.Action == "run.claimed" || r.Action == "run.taken_over") && r.Options["dir"] != "" {
		directory, err := location.Canonical(r.Options["dir"])
		if err != nil {
			return nil, failure("invalid_argument", "Cannot resolve directory.")
		}
		options := make(map[string]string, len(r.Options))
		for key, value := range r.Options {
			options[key] = value
		}
		options["dir"] = directory
		r.Options = options
	}
	b, marshalErr := json.Marshal(r.Body)
	if marshalErr != nil {
		return nil, failure("invalid_argument", "Invalid request body.")
	}
	if r.Body == nil {
		b = []byte("{}")
	}
	body, bodyErr := Decode(string(b))
	if bodyErr != nil {
		return nil, bodyErr
	}
	r.Body = body
	if e := validateBody(def, body); e != nil {
		return nil, e
	}
	if r.Action == "workstream.edited" && r.Options["dry-run"] == "true" {
		profileRelease, lockErr := LockShared(ctx, s.path()+".lock")
		if lockErr != nil {
			return nil, gateError(ctx)
		}
		defer profileRelease()
		_, state, e := s.load()
		if e != nil {
			return nil, e
		}
		if r.Options["if-revision"] != fmt.Sprint(state.Revision) {
			return nil, state.explain(r, failure("revision_conflict", "Profile changed; read the latest revision."), s.Profile)
		}
		evaluation, e := EvaluateEdit(state, r.Target, r.Body, nil)
		if e != nil {
			return nil, state.explain(r, e, s.Profile)
		}
		out := evaluation.Result
		delete(out, "target_id")
		out["profile"] = s.Profile
		out["previous_revision"] = state.Revision
		out["revision"] = state.Revision
		out["current_revision"] = state.Revision
		out["changed"] = false
		out["affected_ids"] = []string{}
		out["affected_count"] = 0
		out["action_ids"] = []string{}
		out["request_id"] = r.Options["request-id"]
		out["replayed"] = false
		return out, nil
	}
	if !validID(r.Options["request-id"]) {
		return nil, failure("invalid_argument", "Provide a UUID request-id.")
	}
	releaseProfile, e := LockExclusive(ctx, s.path()+".lock")
	if e != nil {
		if ctx.Err() != nil {
			return nil, canceledError()
		}
		return nil, storageError()
	}
	defer func() {
		if releaseProfile != nil {
			releaseProfile()
		}
	}()
	if orphanErr := s.CollectOrphanGenerationsHeld(); orphanErr != nil {
		return nil, storageError()
	}
	probed, probeErr := s.resolveStorage()
	if probeErr != nil {
		return nil, probeErr
	}
	if probed.V3 != nil {
		if prepareErr := prepareWALForMutation(*probed.V3); prepareErr != nil {
			return nil, storageError()
		}
		if recoverErr := recoverPendingV3(*probed.V3); recoverErr != nil {
			return nil, storageError()
		}
	}
	var (
		j       *Journal
		state   *State
		storage taskStorageInfo
		err     *protocol.Error
	)
	if probed.V3 != nil {
		storage = probed
		current, needsCheckpoint, currentErr := loadCurrentV3ForMutation(*probed.V3)
		if currentErr != nil {
			return nil, storageError()
		}
		if needsCheckpoint {
			if checkpointErr := checkpointV3(*probed.V3, current); checkpointErr != nil {
				return nil, storageError()
			}
		}
		state = current
	} else {
		j, state, storage, err = s.loadResolved()
		if err != nil {
			return nil, err
		}
		if !exclusive {
			releaseProfile()
			releaseProfile = nil
			releaseGate()
			releaseGate = nil
			return s.execute(ctx, r, true)
		}
	}
	contextUsed := state.usesContext(r)
	if !contextUsed {
		options := map[string]string{}
		for key, value := range r.Options {
			if key != "context" {
				options[key] = value
			}
		}
		r.Options = options
	}
	fingerprint := hash(Object{"action": r.Action, "target": r.Target, "body": r.Body, "options": fingerprintOptions(r.Options)})
	token := r.Options["context"]
	var contextLookup ContextLookup
	contextRunID := ""
	if storage.V3 != nil {
		if contextUsed && token != "" {
			contextHash := hash(token)
			payload, exists, readErr := readCommittedContext(*storage.V3, contextHash)
			if readErr != nil {
				return nil, storageError()
			}
			if exists {
				contextRunID = payload.RunID
				contextLookup = func(value string) (string, bool) {
					if value != contextHash {
						return "", false
					}
					return payload.RunID, true
				}
			}
		}
		old, exists, readErr := readCommittedReceipt(*storage.V3, r.Options["request-id"])
		if readErr != nil {
			return nil, storageError()
		}
		if exists {
			if old.Fingerprint != fingerprint || contextUsed && old.ContextHash != hash(token) {
				return nil, failure("request_conflict", "This request ID was used with different input.")
			}
			result := copyObject(old.Result)
			if result["changed"] == false && result["claimed"] != false {
				return nil, state.noChangeFailure(r, s.Profile)
			}
			result["replayed"] = true
			result["current_revision"] = state.Revision
			if contextUsed {
				current := state.Runs[contextRunID]
				valid := current != nil && current.State == "running"
				result["context_valid"] = valid
				if !valid {
					if _, exists := result["context"]; exists {
						result["context"] = nil
					}
				}
			} else if run, ok := result["run"].(map[string]any); ok {
				current := state.Runs[str(run, "id")]
				valid := current != nil && current.State == "running"
				result["context_valid"] = valid
				if !valid {
					result["context"] = nil
				}
			}
			return result, nil
		}
	} else {
		contextLookup = mapContextLookup(j.Contexts)
		if old, ok := j.Receipts[r.Options["request-id"]]; ok {
			if old.Fingerprint != fingerprint || contextUsed && old.ContextHash != hash(token) {
				return nil, failure("request_conflict", "This request ID was used with different input.")
			}
			result := copyObject(old.Result)
			if result["changed"] == false && result["claimed"] != false {
				return nil, state.noChangeFailure(r, s.Profile)
			}
			result, err = normalizeReceipt(result, state)
			if err != nil {
				return nil, err
			}
			result["replayed"] = true
			result["current_revision"] = state.Revision
			if contextUsed {
				current := state.Runs[j.Contexts[hash(token)]]
				valid := current != nil && current.State == "running"
				result["context_valid"] = valid
				if !valid {
					if _, exists := result["context"]; exists {
						result["context"] = nil
					}
				}
			} else if run, ok := result["run"].(map[string]any); ok {
				current := state.Runs[str(run, "id")]
				valid := current != nil && current.State == "running"
				result["context_valid"] = valid
				if !valid {
					result["context"] = nil
				}
			}
			return result, nil
		}
	}
	prepared := state
	if state.Version == 1 {
		prepared = state.clone()
		prepared.applyUpgrade(state.upgradeEvent())
	}
	events, result, credential, err := prepared.prepare(r, contextLookup)
	if err != nil {
		return nil, prepared.explain(r, err, s.Profile)
	}
	if len(events) == 0 && !(r.Action == "run.claimed" && result["claimed"] == false) {
		return nil, prepared.noChangeFailure(r, s.Profile)
	}
	if ctx.Err() != nil {
		return nil, canceledError()
	}
	previousRevision := state.Revision
	before := state.clone()
	ids := []string{}
	committedEvents := make([]Event, 0, len(events)+1)
	if len(events) > 0 && state.Version == 1 {
		events = append([]Event{state.upgradeEvent()}, events...)
		if j != nil {
			j.Version = JournalVersion
		}
	}
	for _, e := range events {
		e.ID = ID()
		e.Sequence = state.Revision + 1
		e.RequestID = r.Options["request-id"]
		e.At = stamp()
		b, _ := json.Marshal(e)
		_ = json.Unmarshal(b, &e)
		state.Apply(e)
		if j != nil {
			j.Events = append(j.Events, e)
		}
		committedEvents = append(committedEvents, e)
		ids = append(ids, e.ID)
		for _, key := range []string{"basis_id", "record_id"} {
			if id := str(e.Data, key); id != "" {
				result[key] = id
			}
		}
	}
	newContexts := map[string]string{}
	triggerContextHash := ""
	if credential != "" {
		run := state.Current(str(result, "target_id"))
		triggerContextHash = hash(credential)
		if j != nil {
			j.Contexts[triggerContextHash] = run.ID
		}
		newContexts[triggerContextHash] = run.ID
		result["run"] = run
		result["context"] = credential
		result["context_valid"] = true
	}
	if contextUsed {
		result["context_valid"] = r.Action != "run.released" && r.Action != "task.completed"
	}
	if id := str(result, "target_id"); id != "" {
		if i := state.Items[id]; i != nil {
			result["item"] = state.View(i)
		}
	}
	delete(result, "target_id")
	result["profile"] = s.Profile
	result["previous_revision"] = previousRevision
	result["revision"] = state.Revision
	result["current_revision"] = state.Revision
	result["request_id"] = r.Options["request-id"]
	result["replayed"] = false
	result["changed"] = len(events) > 0
	result["action_ids"] = ids
	affected := changedItemIDs(before, state)
	if len(events) > 0 && len(affected) == 0 {
		return nil, before.noChangeFailure(r, s.Profile)
	}
	result["affected_ids"] = affected
	result["affected_count"] = len(affected)
	if r.Action == "workstream.edited" {
		raw, _ := json.Marshal(result)
		if len(raw) > 2<<20 {
			return nil, failure("invalid_argument", "Edit result exceeds 2 MiB; split the request.")
		}
	}
	contextHash := ""
	if contextUsed {
		contextHash = hash(token)
	}
	if j != nil {
		j.Receipts[r.Options["request-id"]] = Receipt{Fingerprint: fingerprint, ContextHash: contextHash, Result: result}
	}
	if storage.V3 != nil {
		if err := commitActiveV3(*storage.V3, state, committedEvents, previousRevision, r.Options["request-id"], fingerprint, contextHash, result, newContexts, s.commitV3); err != nil {
			return nil, storageError()
		}
		return result, nil
	}
	if err := s.migrateToV3(j, state, storage, previousRevision, r.Options["request-id"], triggerContextHash); err != nil {
		return nil, storageError()
	}
	return result, nil
}

func changedItemIDs(before, after *State) []string {
	seen := map[string]bool{}
	for id := range before.Items {
		seen[id] = true
	}
	for id := range after.Items {
		seen[id] = true
	}
	ids := []string{}
	for id := range seen {
		if hash(before.Items[id]) != hash(after.Items[id]) || hash(before.Tracking[id]) != hash(after.Tracking[id]) {
			ids = append(ids, id)
		}
	}
	return unique(ids)
}

func normalizeReceipt(result Object, state *State) (Object, *protocol.Error) {
	applied := num(result, "revision")
	if _, ok := result["previous_revision"]; !ok {
		result["previous_revision"] = applied - len(arr(result, "action_ids"))
	}
	if _, ok := result["affected_ids"]; !ok {
		before, e := snapshotAt(state, num(result, "previous_revision"))
		if e != nil {
			return nil, e
		}
		after, e := snapshotAt(state, applied)
		if e != nil {
			return nil, e
		}
		affected := changedItemIDs(before, after)
		result["affected_ids"] = affected
		result["affected_count"] = len(affected)
	} else if _, ok := result["affected_count"]; !ok {
		result["affected_count"] = len(arr(result, "affected_ids"))
	}
	return result, nil
}

func snapshotAt(state *State, revision int) (*State, *protocol.Error) {
	if revision < 0 || revision > state.Revision {
		return nil, storageError()
	}
	out := NewState()
	for _, event := range state.Events[:revision] {
		if safeApply(out, event) != nil {
			return nil, storageError()
		}
	}
	return out, nil
}

func fingerprintOptions(o map[string]string) Object {
	r := Object{}
	for k, v := range o {
		if k != "request-id" && k != "context" && k != "file" && k != "stdin" {
			r[k] = v
		}
	}
	return r
}
