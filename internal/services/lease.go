package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

const legacyStartupGrace = 10 * time.Second

type launchMarker struct {
	Version      int    `json:"version"`
	LeaseHandoff string `json:"lease_handoff"`
}

type lockedLease struct {
	file *os.File
}

func (l *lockedLease) closeReference() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *lockedLease) unlockClose() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func privateRegular(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0
}

func tryLease(path string, create bool) (*lockedLease, bool, error) {
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		flags |= os.O_CREATE
		if err := tasks.PrivateDir(filepath.Dir(path)); err != nil {
			return nil, false, err
		}
	}
	file, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, false, err
	}
	if !privateRegular(file) {
		_ = file.Close()
		return nil, false, errors.New("private lease required")
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return &lockedLease{file: file}, true, nil
	}
	_ = file.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
		return nil, false, nil
	}
	return nil, false, err
}

func validateLeaseIdentity(file *os.File, path string) error {
	if file == nil || !privateRegular(file) {
		return errors.New("invalid inherited lease")
	}
	expected, err := os.Lstat(path)
	if err != nil || !expected.Mode().IsRegular() || expected.Mode().Perm()&0077 != 0 {
		return errors.New("invalid lease path")
	}
	actual, err := file.Stat()
	if err != nil || !os.SameFile(expected, actual) {
		return errors.New("lease identity mismatch")
	}
	return nil
}

func adoptInheritedLease(file *os.File, path string) (*lockedLease, error) {
	if err := validateLeaseIdentity(file, path); err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.New("inherited lease unavailable")
	}
	syscall.CloseOnExec(int(file.Fd()))
	return &lockedLease{file: file}, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func validLaunchMarker(marker launchMarker) bool {
	return marker.Version == 1 && marker.LeaseHandoff == "inherited-fd"
}

func (s Store) readLaunchMarker(id string) (launchMarker, bool, *protocol.Error) {
	var marker launchMarker
	err := tasks.ReadPrivate(s.path(id, "launch.json"), &marker)
	if errors.Is(err, os.ErrNotExist) {
		return marker, false, nil
	}
	if err != nil || !validLaunchMarker(marker) {
		return marker, false, storageError()
	}
	return marker, true, nil
}

func (s Store) leaseAvailable(ctx context.Context, id string, allowMissing bool) (bool, *protocol.Error) {
	if ctx.Err() != nil {
		return false, failure("canceled")
	}
	lease, acquired, err := tryLease(s.path(id, "lease.lock"), false)
	if errors.Is(err, os.ErrNotExist) {
		if allowMissing {
			return true, nil
		}
		return false, storageError()
	}
	if err != nil {
		return false, storageError()
	}
	if acquired {
		if err := lease.unlockClose(); err != nil {
			return false, storageError()
		}
	}
	return acquired, nil
}

func (s Store) claimLease(ctx context.Context, id string, allowCreate bool) (*lockedLease, bool, *protocol.Error) {
	if ctx.Err() != nil {
		return nil, false, failure("canceled")
	}
	lease, acquired, err := tryLease(s.path(id, "lease.lock"), allowCreate)
	if errors.Is(err, os.ErrNotExist) && !allowCreate {
		return nil, false, storageError()
	}
	if err != nil {
		return nil, false, storageError()
	}
	return lease, acquired, nil
}

func legacyWithinStartupGrace(record Record, now time.Time) bool {
	if record.State != "starting" || record.CreatedAt.IsZero() {
		return false
	}
	return now.Before(record.CreatedAt.Add(legacyStartupGrace))
}

func (s Store) supervisorLost(ctx context.Context, record Record, respectStartupGrace bool) (bool, *protocol.Error) {
	_, hasMarker, markerErr := s.readLaunchMarker(record.ID)
	if markerErr != nil {
		return false, markerErr
	}
	if record.StartedAt == nil && respectStartupGrace && !hasMarker && legacyWithinStartupGrace(record, time.Now().UTC()) {
		return false, nil
	}
	available, leaseErr := s.leaseAvailable(ctx, record.ID, !hasMarker)
	if leaseErr != nil {
		return false, leaseErr
	}
	return available, nil
}

func (s Store) terminalizeLost(ctx context.Context, record Record, respectStartupGrace bool) (Record, bool, *protocol.Error) {
	_, hasMarker, markerErr := s.readLaunchMarker(record.ID)
	if markerErr != nil {
		return record, false, markerErr
	}
	if record.StartedAt == nil && respectStartupGrace && !hasMarker && legacyWithinStartupGrace(record, time.Now().UTC()) {
		return record, false, nil
	}

	lease, acquired, leaseErr := s.claimLease(ctx, record.ID, !hasMarker)
	if leaseErr != nil {
		return record, false, leaseErr
	}
	if !acquired {
		return record, false, nil
	}
	defer func() { _ = lease.unlockClose() }()

	current, readErr := s.read(record.ID)
	if readErr != nil {
		return record, false, readErr
	}
	if current.EndedAt != nil {
		return current, false, nil
	}
	_, currentHasMarker, currentMarkerErr := s.readLaunchMarker(current.ID)
	if currentMarkerErr != nil {
		return current, false, currentMarkerErr
	}
	if current.StartedAt == nil && respectStartupGrace && !currentHasMarker && legacyWithinStartupGrace(current, time.Now().UTC()) {
		return current, false, nil
	}
	now := time.Now().UTC()
	current.EndedAt = &now
	current.State = "interrupted"
	current.Reason = "supervisor_lost"
	if err := writePrivate(s.path(current.ID, "record.json"), current); err != nil {
		return current, false, storageError()
	}
	return current, true, nil
}

func launchStagingName(id string) string {
	return ".launch-" + id + "-"
}

func validLaunchStagingName(name string) bool {
	if !strings.HasPrefix(name, ".launch-") {
		return false
	}
	rest := strings.TrimPrefix(name, ".launch-")
	if len(rest) <= 37 || !validID(rest[:36]) || rest[36] != '-' {
		return false
	}
	for _, r := range rest[37:] {
		if r < '0' || r > '9' && r < 'A' || r > 'Z' && r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func (s Store) cleanupLaunchStaging() *protocol.Error {
	root := s.root()
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storageError()
	}
	removed := false
	for _, entry := range entries {
		if !validLaunchStagingName(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return storageError()
		}
		if err := os.RemoveAll(path); err != nil {
			return storageError()
		}
		removed = true
	}
	if removed && syncDirectory(root) != nil {
		return storageError()
	}
	return nil
}

func (s Store) prepareLaunch(record Record) (*lockedLease, *protocol.Error) {
	if err := tasks.PrivateDir(s.root()); err != nil {
		return nil, storageError()
	}
	staging, err := os.MkdirTemp(s.root(), launchStagingName(record.ID))
	if err != nil {
		return nil, storageError()
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
			_ = syncDirectory(s.root())
		}
	}()

	lease, acquired, err := tryLease(filepath.Join(staging, "lease.lock"), true)
	if err != nil || !acquired {
		return nil, storageError()
	}
	keepLease := false
	defer func() {
		if !keepLease {
			_ = lease.unlockClose()
		}
	}()

	if err := writePrivate(filepath.Join(staging, "record.json"), record); err != nil {
		return nil, storageError()
	}
	if err := writePrivate(filepath.Join(staging, "launch.json"), launchMarker{Version: 1, LeaseHandoff: "inherited-fd"}); err != nil {
		return nil, storageError()
	}
	if err := syncDirectory(staging); err != nil {
		return nil, storageError()
	}

	target := filepath.Join(s.root(), record.ID)
	if _, err := os.Lstat(target); err == nil {
		return nil, failure("request_conflict")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, storageError()
	}
	if err := os.Rename(staging, target); err != nil {
		return nil, storageError()
	}
	published = true
	if err := syncDirectory(s.root()); err != nil {
		return nil, storageError()
	}
	if err := validateLeaseIdentity(lease.file, s.path(record.ID, "lease.lock")); err != nil {
		return nil, storageError()
	}
	keepLease = true
	return lease, nil
}
