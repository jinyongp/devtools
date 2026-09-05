// Package ports persists local TCP assignments and execution-location identities.
package ports

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Instance struct {
	Profile   string  `json:"profile"`
	ID        string  `json:"instance_id"`
	Alias     *string `json:"alias"`
	Directory string  `json:"directory"`
}
type Assignment struct {
	Instance
	Name string `json:"name"`
	Port int    `json:"port"`
}
type State struct {
	Version     int          `json:"version"`
	Instances   []Instance   `json:"instances"`
	Assignments []Assignment `json:"assignments"`
}
type Store struct{ Directory string }

func fail(code string) *protocol.Error {
	return protocol.NewError(code, "Port operation could not satisfy the requested condition.", 3, nil)
}
func storageError() *protocol.Error {
	return protocol.NewError("io_error", "Cannot access private port storage or inspect local ports.", 1, nil)
}
func empty() *State { return &State{Version: 1, Instances: []Instance{}, Assignments: []Assignment{}} }
func private(f *os.File) bool {
	i, e := f.Stat()
	return e == nil && i.Mode().IsRegular() && i.Mode().Perm()&0077 == 0
}
func (s Store) Read() (*State, *protocol.Error) {
	i, e := os.Lstat(s.Directory)
	if errors.Is(e, os.ErrNotExist) {
		return empty(), nil
	}
	if e != nil || !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return nil, storageError()
	}
	f, e := os.OpenFile(filepath.Join(s.Directory, "registry.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(e, os.ErrNotExist) {
		return empty(), nil
	}
	if e != nil {
		return nil, storageError()
	}
	defer f.Close()
	if !private(f) {
		return nil, storageError()
	}
	d := json.NewDecoder(io.LimitReader(f, 16<<20))
	d.DisallowUnknownFields()
	var st State
	if d.Decode(&st) != nil || d.Decode(new(any)) != io.EOF || !st.valid() {
		return nil, fail("invalid_storage")
	}
	return &st, nil
}
func (st *State) valid() bool {
	if st.Version != 1 || st.Instances == nil || st.Assignments == nil {
		return false
	}
	ids, paths, aliases, keys, nums := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[int]bool{}
	for _, i := range st.Instances {
		b, e := hex.DecodeString(i.ID)
		if e != nil || len(b) != 16 || !project.ValidProfile(i.Profile) || !filepath.IsAbs(i.Directory) || ids[i.ID] || paths[i.Profile+"\x00"+i.Directory] {
			return false
		}
		ids[i.ID] = true
		paths[i.Profile+"\x00"+i.Directory] = true
		if i.Alias != nil {
			k := i.Profile + "\x00" + *i.Alias
			if !project.ValidProfile(*i.Alias) || aliases[k] {
				return false
			}
			aliases[k] = true
		}
	}
	for _, a := range st.Assignments {
		i := st.ByID(a.ID)
		k := a.ID + "\x00" + a.Name
		if i == nil || i.Profile != a.Profile || i.Directory != a.Directory || !sameAlias(i.Alias, a.Alias) || !project.ValidProfile(a.Name) || a.Port < 1 || a.Port > 65535 || keys[k] || nums[a.Port] {
			return false
		}
		keys[k] = true
		nums[a.Port] = true
	}
	return true
}
func sameAlias(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func (s Store) lock(ctx context.Context, name string, wait bool, create bool) (*os.File, *protocol.Error) {
	if create {
		if os.MkdirAll(s.Directory, 0700) != nil {
			return nil, storageError()
		}
	}
	i, e := os.Lstat(s.Directory)
	if errors.Is(e, os.ErrNotExist) && !create {
		return nil, nil
	}
	if e != nil || !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return nil, storageError()
	}
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		flags |= os.O_CREATE
	}
	f, e := os.OpenFile(filepath.Join(s.Directory, name), flags, 0600)
	if errors.Is(e, os.ErrNotExist) && !create {
		return nil, nil
	}
	if e != nil {
		return nil, storageError()
	}
	if !private(f) {
		f.Close()
		return nil, storageError()
	}
	for {
		if ctx.Err() != nil {
			f.Close()
			return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return f, nil
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EINTR) {
			f.Close()
			return nil, storageError()
		}
		if !wait {
			f.Close()
			return nil, fail("port_run_active")
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func (s Store) Update(ctx context.Context, fn func(*State) (bool, *protocol.Error)) *protocol.Error {
	l, e := s.lock(ctx, "registry.lock", true, true)
	if e != nil {
		return e
	}
	defer l.Close()
	st, e := s.Read()
	if e != nil {
		return e
	}
	changed, e := fn(st)
	if e != nil || !changed {
		return e
	}
	if !st.valid() {
		return fail("invalid_storage")
	}
	b, err := json.Marshal(st)
	if err != nil {
		return storageError()
	}
	f, err := os.CreateTemp(s.Directory, ".update-*")
	if err != nil {
		return storageError()
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return storageError()
	}
	if os.Rename(f.Name(), filepath.Join(s.Directory, "registry.json")) != nil {
		return storageError()
	}
	d, err := os.Open(s.Directory)
	if err != nil {
		return storageError()
	}
	defer d.Close()
	if d.Sync() != nil {
		return storageError()
	}
	return nil
}
func (st *State) ByID(id string) *Instance {
	for n := range st.Instances {
		if st.Instances[n].ID == id {
			return &st.Instances[n]
		}
	}
	return nil
}
func (st *State) Find(profile, selector, dir string) *Instance {
	for n := range st.Instances {
		i := &st.Instances[n]
		if i.Profile == profile && (selector != "" && (selector == "id:"+i.ID || i.Alias != nil && selector == *i.Alias) || selector == "" && i.Directory == dir) {
			return i
		}
	}
	return nil
}
func (st *State) Register(profile, dir string) (*Instance, *protocol.Error) {
	if i := st.Find(profile, "", dir); i != nil {
		return i, nil
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return nil, storageError()
	}
	st.Instances = append(st.Instances, Instance{Profile: profile, ID: hex.EncodeToString(b), Directory: dir})
	return &st.Instances[len(st.Instances)-1], nil
}
func (st *State) Get(id, name string) *Assignment {
	for n := range st.Assignments {
		if st.Assignments[n].ID == id && st.Assignments[n].Name == name {
			return &st.Assignments[n]
		}
	}
	return nil
}
func (st *State) SyncInstance(i Instance) {
	for n := range st.Assignments {
		if st.Assignments[n].ID == i.ID {
			st.Assignments[n].Instance = i
		}
	}
}
func runLock(id, name string) string {
	return "run-" + id + "-" + hex.EncodeToString([]byte(name)) + ".lock"
}
func (s Store) Active(ctx context.Context, id, name string) *protocol.Error {
	f, e := s.lock(ctx, runLock(id, name), false, false)
	if f != nil {
		f.Close()
	}
	return e
}
func (s Store) Claim(ctx context.Context, id string, names []string) (func(), *protocol.Error) {
	files := []*os.File{}
	release := func() {
		for _, f := range files {
			f.Close()
		}
	}
	for _, name := range names {
		f, e := s.lock(ctx, runLock(id, name), false, true)
		if e != nil {
			release()
			return nil, e
		}
		files = append(files, f)
	}
	return release, nil
}
