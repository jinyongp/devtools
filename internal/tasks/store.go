package tasks

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
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
	// Test seam for failures before the atomic journal replacement.
	commit func(string, any) error
}

func (s Store) path() string {
	return filepath.Join(s.Directory, hex.EncodeToString([]byte(s.Profile))+".json")
}
func storageError() *protocol.Error {
	return protocol.NewError("storage_error", "Cannot access private task storage.", 1, nil)
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
	if e := PrivateDir(filepath.Dir(path)); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("private lock required")
	}
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EINTR) {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func (s Store) load() (*Journal, *State, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return nil, nil, failure("invalid_argument", "Invalid profile.")
	}
	j := &Journal{Version: 1, Profile: s.Profile, Events: []Event{}, Receipts: map[string]Receipt{}, Contexts: map[string]string{}}
	if info, e := os.Lstat(s.Directory); e == nil {
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, nil, storageError()
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, nil, storageError()
	}
	if e := ReadPrivate(s.path(), j); e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, nil, storageError()
	}
	if j.Profile != s.Profile || j.Receipts == nil || j.Contexts == nil {
		return nil, nil, storageError()
	}
	state, err := replayJournal(j)
	return j, state, err
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
func (s Store) Read() (*State, *protocol.Error) {
	release, e := maintenance.Acquire(context.Background(), maintenance.Root(s.Directory))
	if e != nil {
		return nil, storageError()
	}
	defer release()
	_, state, err := s.load()
	return state, err
}

// CompletionIDs reads an atomic journal snapshot, without query caches or locks.
func (s Store) CompletionIDs(kind, workstream string) ([]string, *protocol.Error) {
	_, state, e := s.load()
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
func (s Store) Profiles() ([]string, *protocol.Error) {
	release, gateErr := maintenance.Acquire(context.Background(), maintenance.Root(s.Directory))
	if gateErr != nil {
		return nil, storageError()
	}
	defer release()
	entries, e := os.ReadDir(s.Directory)
	if errors.Is(e, os.ErrNotExist) {
		return []string{}, nil
	}
	if e != nil {
		return nil, storageError()
	}
	r := []string{}
	for _, f := range entries {
		if filepath.Ext(f.Name()) != ".json" {
			continue
		}
		b, e := hex.DecodeString(f.Name()[:len(f.Name())-5])
		if e == nil && project.ValidProfile(string(b)) {
			r = append(r, string(b))
		}
	}
	return r, nil
}

type Request struct {
	Action  string
	Target  string
	Body    Object
	Options map[string]string
}

func (s Store) Execute(ctx context.Context, r Request) (Object, *protocol.Error) {
	releaseGate, gateErr := maintenance.Acquire(ctx, maintenance.Root(s.Directory))
	if gateErr != nil {
		return nil, storageError()
	}
	defer releaseGate()
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
	release, e := Lock(ctx, s.path()+".lock")
	if e != nil {
		if ctx.Err() != nil {
			return nil, protocol.NewError("canceled", "Request canceled.", 130, nil)
		}
		return nil, storageError()
	}
	defer release()
	j, state, err := s.load()
	if err != nil {
		return nil, err
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
	prepared := state
	if state.Version == 1 {
		prepared = state.clone()
		prepared.applyUpgrade(state.upgradeEvent())
	}
	events, result, credential, err := prepared.prepare(r, j.Contexts)
	if err != nil {
		return nil, prepared.explain(r, err, s.Profile)
	}
	if len(events) == 0 && !(r.Action == "run.claimed" && result["claimed"] == false) {
		return nil, prepared.noChangeFailure(r, s.Profile)
	}
	if ctx.Err() != nil {
		return nil, protocol.NewError("canceled", "Request canceled.", 130, nil)
	}
	previousRevision := state.Revision
	before := state.clone()
	ids := []string{}
	if len(events) > 0 && state.Version == 1 {
		events = append([]Event{state.upgradeEvent()}, events...)
		j.Version = JournalVersion
	}
	for _, e := range events {
		e.ID = ID()
		e.Sequence = state.Revision + 1
		e.RequestID = r.Options["request-id"]
		e.At = stamp()
		b, _ := json.Marshal(e)
		_ = json.Unmarshal(b, &e)
		state.Apply(e)
		j.Events = append(j.Events, e)
		ids = append(ids, e.ID)
		for _, key := range []string{"basis_id", "record_id"} {
			if id := str(e.Data, key); id != "" {
				result[key] = id
			}
		}
	}
	if credential != "" {
		run := state.Current(str(result, "target_id"))
		j.Contexts[hash(credential)] = run.ID
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
	j.Receipts[r.Options["request-id"]] = Receipt{Fingerprint: fingerprint, ContextHash: contextHash, Result: result}
	commit := s.commit
	if commit == nil {
		commit = WritePrivate
	}
	if e := commit(s.path(), j); e != nil {
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
