package tasks

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	if j.Version != 1 || j.Profile != s.Profile || j.Receipts == nil || j.Contexts == nil {
		return nil, nil, storageError()
	}
	state := NewState()
	for n, e := range j.Events {
		if e.Sequence != n+1 {
			return nil, nil, storageError()
		}
		if err := safeApply(state, e); err != nil {
			return nil, nil, storageError()
		}
	}
	return j, state, nil
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
	fingerprint := hash(Object{"action": r.Action, "target": r.Target, "body": r.Body, "options": fingerprintOptions(r.Options)})
	token := r.Options["context"]
	if old, ok := j.Receipts[r.Options["request-id"]]; ok {
		if old.Fingerprint != fingerprint || old.ContextHash != hash(token) {
			return nil, failure("request_conflict", "This request ID was used with different input.")
		}
		result := old.Result
		result["replayed"] = true
		result["current_revision"] = state.Revision
		if run, ok := result["run"].(map[string]any); ok {
			current := state.Runs[str(run, "id")]
			valid := current != nil && current.State == "running"
			result["context_valid"] = valid
			if !valid {
				result["context"] = nil
			}
		}
		return result, nil
	}
	events, result, credential, err := state.prepare(r, j.Contexts)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, protocol.NewError("canceled", "Request canceled.", 130, nil)
	}
	ids := []string{}
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
	if def.Context {
		result["context_valid"] = r.Action != "run.released" && r.Action != "task.completed"
	}
	if id := str(result, "target_id"); id != "" {
		if i := state.Items[id]; i != nil {
			result["item"] = state.View(i)
		}
	}
	delete(result, "target_id")
	result["profile"] = s.Profile
	result["revision"] = state.Revision
	result["current_revision"] = state.Revision
	result["request_id"] = r.Options["request-id"]
	result["replayed"] = false
	result["changed"] = len(events) > 0
	result["action_ids"] = ids
	j.Receipts[r.Options["request-id"]] = Receipt{Fingerprint: fingerprint, ContextHash: hash(token), Result: result}
	if e := WritePrivate(s.path(), j); e != nil {
		return nil, storageError()
	}
	return result, nil
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
