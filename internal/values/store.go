package values

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Store struct {
	Directory string
	Profile   string
	// Test seam between a lockless completion read and its stability recheck.
	completionHook func()
}

func storageError() *protocol.Error {
	return protocol.NewError("storage_error", "Cannot access private profile storage.", 1, nil)
}

func canceledError() *protocol.Error {
	return protocol.NewError("canceled", "Execution canceled.", 130, nil)
}

func gateError(ctx context.Context) *protocol.Error {
	if ctx.Err() != nil {
		return canceledError()
	}
	return storageError()
}

func (s Store) file() string {
	return profilekey.CanonicalPath(s.Directory, s.Profile)
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private directory required")
	}
	return nil
}

func privateFile(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0
}

func (s Store) readPath(path string) (*State, *protocol.Error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, storageError()
	}
	defer file.Close()
	if !privateFile(file) {
		return nil, storageError()
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil || !state.valid(s.Profile) {
		return nil, failure("invalid_storage", "Profile storage is malformed or has an unsupported format.")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, failure("invalid_storage", "Profile storage contains trailing data.")
	}
	return &state, nil
}

func (s Store) readResolved() (*State, profilekey.Resolution, *protocol.Error) {
	resolution, err := profilekey.Resolve(s.Directory, s.Profile, "values")
	if err != nil {
		return nil, resolution, storageError()
	}
	switch resolution.Mode {
	case profilekey.ModeNone:
		return newState(s.Profile), resolution, nil
	case profilekey.ModeLegacy:
		state, readErr := s.readPath(resolution.LegacyPath)
		return state, resolution, readErr
	case profilekey.ModeCanonical:
		state, readErr := s.readPath(resolution.CanonicalPath)
		return state, resolution, readErr
	default:
		return nil, resolution, storageError()
	}
}

func (s Store) read() (*State, *protocol.Error) {
	state, _, err := s.readResolved()
	return state, err
}

func sameResolution(left, right profilekey.Resolution) bool {
	return left.Mode == right.Mode &&
		left.CanonicalPath == right.CanonicalPath &&
		left.IdentityPath == right.IdentityPath &&
		left.LegacyPath == right.LegacyPath &&
		left.LegacyStatus == right.LegacyStatus &&
		left.Marker == right.Marker
}

func (s Store) completionState() (*State, *protocol.Error) {
	for attempt := 0; attempt < 2; attempt++ {
		before, err := profilekey.Resolve(s.Directory, s.Profile, "values")
		if err != nil {
			continue
		}
		var state *State
		var readErr *protocol.Error
		switch before.Mode {
		case profilekey.ModeNone:
			state = newState(s.Profile)
		case profilekey.ModeLegacy:
			state, readErr = s.readPath(before.LegacyPath)
		case profilekey.ModeCanonical:
			state, readErr = s.readPath(before.CanonicalPath)
		default:
			readErr = storageError()
		}
		if readErr != nil {
			continue
		}
		if s.completionHook != nil {
			s.completionHook()
		}
		after, err := profilekey.Resolve(s.Directory, s.Profile, "values")
		if err == nil && sameResolution(before, after) {
			return state, nil
		}
	}
	return nil, storageError()
}

func (s Store) Read(ctx context.Context) (*State, *protocol.Error) {
	release, gateErr := maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	if gateErr != nil {
		return nil, gateError(ctx)
	}
	defer release()
	return s.ReadHeld(ctx)
}

// ReadHeld requires the caller to hold the data-root maintenance gate.
func (s Store) ReadHeld(ctx context.Context) (*State, *protocol.Error) {
	if ctx.Err() != nil {
		return nil, canceledError()
	}
	if !project.ValidProfile(s.Profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	info, err := os.Lstat(s.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return newState(s.Profile), nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, storageError()
	}
	return s.read()
}

// CompletionNames reads one atomic snapshot without creating directories or locks.
// Only identifiers cross this boundary; stored values stay inside the values package.
func (s Store) CompletionNames(kind Kind, env string) ([]string, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return nil, storageError()
	}
	info, err := os.Lstat(s.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, storageError()
	}
	state, e := s.completionState()
	if e != nil {
		return nil, e
	}
	if kind == "" {
		return state.EnvNames(), nil
	}
	items, e := state.List(kind, env)
	if e != nil {
		return nil, e
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Key)
	}
	return names, nil
}

// Update serializes read/modify/write per profile. Canonical single-file
// updates use the shared global maintenance gate; first-create/legacy migration
// restarts under the exclusive gate before invoking change.
func (s Store) Update(ctx context.Context, change func(*State) (bool, *protocol.Error)) (bool, *protocol.Error) {
	return s.update(ctx, change, false)
}

func (s Store) update(ctx context.Context, change func(*State) (bool, *protocol.Error), exclusive bool) (bool, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return false, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	var releaseGate func()
	var gateErr error
	if exclusive {
		releaseGate, gateErr = maintenance.AcquireExclusive(ctx, maintenance.Root(s.Directory))
	} else {
		releaseGate, gateErr = maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	}
	if gateErr != nil {
		return false, gateError(ctx)
	}
	if ctx.Err() != nil {
		releaseGate()
		return false, canceledError()
	}

	info, dirErr := os.Lstat(s.Directory)
	if errors.Is(dirErr, os.ErrNotExist) {
		if !exclusive {
			releaseGate()
			return s.update(ctx, change, true)
		}
		if err := privateDirectory(s.Directory); err != nil {
			releaseGate()
			return false, storageError()
		}
	} else if dirErr != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		releaseGate()
		return false, storageError()
	}

	releaseProfile, lockErr := maintenance.LockExclusive(ctx, s.file()+".lock")
	if lockErr != nil {
		releaseGate()
		return false, gateError(ctx)
	}
	state, resolution, readErr := s.readResolved()
	if readErr != nil {
		releaseProfile()
		releaseGate()
		return false, readErr
	}
	if !exclusive && resolution.Mode != profilekey.ModeCanonical {
		releaseProfile()
		releaseGate()
		return s.update(ctx, change, true)
	}
	defer releaseGate()
	defer releaseProfile()

	changed, changeErr := change(state)
	if changeErr != nil || !changed {
		return changed, changeErr
	}
	if ctx.Err() != nil {
		return false, canceledError()
	}
	if !state.valid(s.Profile) {
		return false, failure("invalid_storage", "Profile update is invalid.")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return false, storageError()
	}
	if resolution.Mode != profilekey.ModeCanonical {
		identity, identityErr := profilekey.IdentityBytes(s.Profile, "values")
		if identityErr != nil {
			return false, storageError()
		}
		files := map[string][]byte{
			profilekey.StateRelative("profiles", s.Profile):    data,
			profilekey.IdentityRelative("profiles", s.Profile): identity,
		}
		if resolution.LegacyStatus != profilekey.LegacyUnaddressable {
			migrationMarker, markerErr := profilekey.MarkerBytes(s.Profile)
			if markerErr != nil {
				return false, storageError()
			}
			files[profilekey.LegacyRelative("profiles", s.Profile)] = migrationMarker
		}
		if err := maintenance.Replace(maintenance.Root(s.Directory), files); err != nil {
			return false, storageError()
		}
		return true, nil
	}
	if err := maintenance.Write(resolution.CanonicalPath, data); err != nil {
		return false, storageError()
	}
	return true, nil
}
