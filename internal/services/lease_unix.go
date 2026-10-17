//go:build !windows

package services

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

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

func (s Store) prepareLaunch(record Record, snapshots ...*network.ExecutionSnapshot) (*lockedLease, *protocol.Error) {
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
	if record.ProxyMode {
		if len(snapshots) == 0 || snapshots[0] == nil || snapshots[0].ID != record.NetworkSnapshotID {
			return nil, failure("network_snapshot_required")
		}
		if err := writePrivate(filepath.Join(staging, "network.json"), snapshots[0]); err != nil {
			return nil, storageError()
		}
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

func platformLaunchMarker() launchMarker {
	return launchMarker{Version: 1, LeaseHandoff: "inherited-fd"}
}
func (s Store) leasePath(id string) string { return s.path(id, "lease.lock") }
