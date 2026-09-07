package values

import (
	"bytes"
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

type Store struct {
	Directory string
	Profile   string
}

func storageError() *protocol.Error {
	return protocol.NewError("storage_error", "Cannot access private profile storage.", 1, nil)
}

func (s Store) file() string {
	// Hex names preserve case-sensitive profile identity on case-insensitive disks.
	return filepath.Join(s.Directory, hex.EncodeToString([]byte(s.Profile))+".json")
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

func (s Store) read() (*State, *protocol.Error) {
	file, err := os.OpenFile(s.file(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return newState(s.Profile), nil
	}
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

func (s Store) Read() (*State, *protocol.Error) {
	release, gateErr := maintenance.Acquire(context.Background(), maintenance.Root(s.Directory))
	if gateErr != nil {
		return nil, storageError()
	}
	defer release()
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
	state, e := s.read()
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

// Update serializes read/modify/write and publishes a complete snapshot atomically.
func (s Store) Update(ctx context.Context, change func(*State) (bool, *protocol.Error)) (bool, *protocol.Error) {
	release, gateErr := maintenance.Acquire(ctx, maintenance.Root(s.Directory))
	if gateErr != nil {
		return false, storageError()
	}
	defer release()
	if !project.ValidProfile(s.Profile) {
		return false, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	if ctx.Err() != nil {
		return false, protocol.NewError("canceled", "Execution canceled.", 130, nil)
	}
	if err := privateDirectory(s.Directory); err != nil {
		return false, storageError()
	}
	lock, err := os.OpenFile(s.file()+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return false, storageError()
	}
	defer lock.Close()
	if !privateFile(lock) {
		return false, storageError()
	}
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return false, storageError()
		}
		select {
		case <-ctx.Done():
			return false, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if ctx.Err() != nil {
		return false, protocol.NewError("canceled", "Execution canceled.", 130, nil)
	}
	state, readErr := s.read()
	if readErr != nil {
		return false, readErr
	}
	changed, changeErr := change(state)
	if changeErr != nil || !changed {
		return changed, changeErr
	}
	if !state.valid(s.Profile) {
		return false, failure("invalid_storage", "Profile update is invalid.")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return false, storageError()
	}
	file, err := os.CreateTemp(s.Directory, ".update-*")
	if err != nil {
		return false, storageError()
	}
	defer os.Remove(file.Name())
	_, writeErr := io.Copy(file, bytes.NewReader(data))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return false, storageError()
	}
	if err := os.Rename(file.Name(), s.file()); err != nil {
		return false, storageError()
	}
	dir, err := os.Open(s.Directory)
	if err != nil {
		return false, storageError()
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return false, storageError()
	}
	return true, nil
}
