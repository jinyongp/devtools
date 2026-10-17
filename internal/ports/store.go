// Package ports persists local TCP assignments and execution-location identities.
package ports

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/platformfs"
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
type Reservation struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}
type State struct {
	readIndex    *registryReadIndex
	Version      int           `json:"version"`
	Instances    []Instance    `json:"instances"`
	Assignments  []Assignment  `json:"assignments"`
	Reservations []Reservation `json:"reservations"`
	Network      *NetworkState `json:"network,omitempty"`
}

type NetworkState struct {
	Listeners   []network.Listener    `json:"listeners"`
	Routes      []network.GlobalRoute `json:"routes"`
	Activations []network.Activation  `json:"activations"`
	Pins        []network.Pin         `json:"pins"`
}
type AvailabilityProbe func(int) (bool, *protocol.Error)

type Store struct {
	Directory string
	Probe     AvailabilityProbe
}

func fail(code string) *protocol.Error {
	return protocol.NewError(code, "Port operation could not satisfy the requested condition.", 3, nil)
}
func storageError() *protocol.Error {
	return protocol.NewError("io_error", "Cannot access private port storage or inspect local ports.", 1, nil)
}
func empty() *State {
	return &State{Version: 1, Instances: []Instance{}, Assignments: []Assignment{}, Reservations: []Reservation{}}
}
func private(f *os.File) bool {
	return platformfs.CheckFile(f) == nil
}
func (s Store) Read() (*State, *protocol.Error) {
	i, e := os.Lstat(s.Directory)
	if errors.Is(e, os.ErrNotExist) {
		return empty(), nil
	}
	if e != nil || !platformfs.DirectoryPrivate(s.Directory, i) {
		return nil, storageError()
	}
	f, e := platformfs.OpenPrivate(filepath.Join(s.Directory, "registry.json"), os.O_RDONLY, 0)
	if errors.Is(e, os.ErrNotExist) {
		return empty(), nil
	}
	if e != nil {
		return nil, storageError()
	}
	defer f.Close()
	if !private(f.File) {
		return nil, storageError()
	}
	if info, err := f.Stat(); err != nil || info.Size() > registryDocumentLimit {
		return nil, fail("invalid_storage")
	}
	d := json.NewDecoder(io.LimitReader(f, registryDocumentLimit))
	d.DisallowUnknownFields()
	var st State
	if d.Decode(&st) != nil || d.Decode(new(any)) != io.EOF {
		return nil, fail("invalid_storage")
	}
	index, ok := st.validatedIndex()
	if !ok {
		return nil, fail("invalid_storage")
	}
	st.readIndex = index
	return &st, nil
}
func (state *State) valid() bool {
	_, ok := state.validatedIndex()
	return ok
}

func (state *State) validatedIndex() (*registryReadIndex, bool) {
	st := *state
	st.readIndex = buildRegistryReadIndex(state)
	if st.Version < 1 || st.Version > registryVersion || st.Instances == nil || st.Assignments == nil || st.Version >= 2 && st.Reservations == nil || st.Version == 1 && len(st.Reservations) != 0 || st.Version < 3 && st.Network != nil || st.Version == 3 && st.Network == nil {
		return nil, false
	}
	index := st.readIndex
	if len(index.instances) != len(st.Instances) || len(index.directories) != len(st.Instances) || len(index.assignments) != len(st.Assignments) || len(index.reservations) != len(st.Reservations) {
		return nil, false
	}
	aliasCount := 0
	nums := map[int]bool{}
	for _, i := range st.Instances {
		b, e := hex.DecodeString(i.ID)
		if e != nil || len(b) != 16 || !project.ValidProfile(i.Profile) || !filepath.IsAbs(i.Directory) {
			return nil, false
		}
		if i.Alias != nil {
			aliasCount++
			if !project.ValidProfile(*i.Alias) {
				return nil, false
			}
		}
	}
	if aliasCount != len(index.aliases) {
		return nil, false
	}
	for _, a := range st.Assignments {
		i := st.ByID(a.ID)
		if i == nil || i.Profile != a.Profile || i.Directory != a.Directory || !sameAlias(i.Alias, a.Alias) || !project.ValidProfile(a.Name) || a.Port < 1 || a.Port > 65535 || nums[a.Port] {
			return nil, false
		}
		nums[a.Port] = true
	}
	for _, reservation := range st.Reservations {
		if !project.ValidProfile(reservation.Name) || reservation.Port < 1 || reservation.Port > 65535 || nums[reservation.Port] {
			return nil, false
		}
		nums[reservation.Port] = true
	}
	if !st.validNetwork() {
		return nil, false
	}
	return index, true
}
func sameAlias(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

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
	pinned := st.pinnedProjections()
	st.readIndex = nil
	changed, e := fn(st)
	if e != nil || !changed {
		return e
	}
	st.joinAssignments()
	for id, current := range st.pinnedProjections() {
		if previous, existed := pinned[id]; existed && previous != current {
			return fail("pinned")
		}
	}
	if st.Version == 1 {
		st.Version = 2
		if st.Reservations == nil {
			st.Reservations = []Reservation{}
		}
	}
	if !st.valid() {
		return fail("invalid_storage")
	}
	st.Version = registryVersion
	b, err := json.Marshal(st)
	if err != nil {
		return storageError()
	}
	if len(b) > registryDocumentLimit {
		return fail("invalid_storage")
	}
	return s.publishRegistry(b)
}

func (st *State) ByID(id string) *Instance {
	if st.readIndex != nil {
		if n, ok := st.readIndex.instances[id]; ok {
			return &st.Instances[n]
		}
		return nil
	}
	for n := range st.Instances {
		if st.Instances[n].ID == id {
			return &st.Instances[n]
		}
	}
	return nil
}
func (st *State) Find(profile, selector, dir string) *Instance {
	if st.readIndex != nil {
		if len(selector) > 3 && selector[:3] == "id:" {
			if i := st.ByID(selector[3:]); i != nil && i.Profile == profile {
				return i
			}
			return nil
		}
		indices, key := st.readIndex.aliases, [2]string{profile, selector}
		if selector == "" {
			indices, key = st.readIndex.directories, [2]string{profile, dir}
		}
		if n, ok := indices[key]; ok {
			return &st.Instances[n]
		}
		return nil
	}
	for n := range st.Instances {
		i := &st.Instances[n]
		if i.Profile == profile && (selector != "" && (selector == "id:"+i.ID || i.Alias != nil && selector == *i.Alias) || selector == "" && i.Directory == dir) {
			return i
		}
	}
	return nil
}
func (st *State) Register(profile, dir string) (*Instance, *protocol.Error) {
	st.readIndex = nil
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
	if st.readIndex != nil {
		if n, ok := st.readIndex.assignments[[2]string{id, name}]; ok {
			return &st.Assignments[n]
		}
		return nil
	}
	for n := range st.Assignments {
		if st.Assignments[n].ID == id && st.Assignments[n].Name == name {
			return &st.Assignments[n]
		}
	}
	return nil
}
func (st *State) Reservation(name string) *Reservation {
	if st.readIndex != nil {
		if n, ok := st.readIndex.reservations[name]; ok {
			return &st.Reservations[n]
		}
		return nil
	}
	for index := range st.Reservations {
		if st.Reservations[index].Name == name {
			return &st.Reservations[index]
		}
	}
	return nil
}

func runLock(id, name string) string {
	legacy := "run-" + id + "-" + hex.EncodeToString([]byte(name)) + ".lock"
	if len(legacy) <= 255 {
		return legacy
	}
	digest := sha256.Sum256([]byte(name))
	return "run-" + id + "-" + hex.EncodeToString(digest[:]) + ".lock"
}
func (s Store) Active(ctx context.Context, id, name string) *protocol.Error {
	f, e := s.lock(ctx, runLock(id, name), false, false)
	if f != nil {
		f.Close()
	}
	return e
}
func (s Store) Claim(ctx context.Context, id string, names []string) (func(), *protocol.Error) {
	files := []io.Closer{}
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
