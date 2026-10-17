package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"golang.org/x/sys/windows"
)

func platformLaunchMarker() launchMarker {
	return launchMarker{Version: 2, LeaseHandoff: "inherited-handle"}
}
func (s Store) leasePath(id string) string {
	return filepath.Join(s.Data, "process-leases", id+".lock")
}
func (l *lockedLease) unlockClose() error {
	if l == nil {
		return nil
	}
	return l.closeReference()
}

func tryLease(path string, create bool) (*lockedLease, bool, error) {
	root, err := platformfs.OpenRoot(filepath.Dir(path), create)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	file, err := root.OpenExclusive(filepath.Base(path), create, true)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &lockedLease{file: file.File, closeGuard: file.Close}, true, nil
}
func validateLeaseIdentity(file *os.File, path string) error {
	root, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.ValidateFileHandle(filepath.Base(path), file); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() != 0 {
		return platformfs.ErrUnsafe
	}
	// A metadata-only handle names the same object but cannot own the launch
	// lease. Verify data access without changing the shared file position.
	var one [1]byte
	if n, err := file.ReadAt(one[:], 0); n != 0 || !errors.Is(err, io.EOF) {
		return platformfs.ErrUnsafe
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	probe, err := windows.CreateFile(ptr, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err == nil {
		_ = windows.CloseHandle(probe)
		return platformfs.ErrUnsafe
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return err
	}
	return nil
}
func adoptInheritedLease(file *os.File, path string) (*lockedLease, error) {
	if err := validateLeaseIdentity(file, path); err != nil {
		return nil, err
	}
	root, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	inherited, err := root.AdoptFile(filepath.Base(path), file)
	if err != nil {
		return nil, err
	}
	return &lockedLease{file: inherited.File, closeGuard: inherited.Close}, nil
}
func (s Store) readLaunchMarker(id string) (launchMarker, bool, *protocol.Error) {
	var marker launchMarker
	if !validID(id) {
		return marker, false, storageErrorAt("validate_launch_marker_id", nil)
	}
	root, err := platformfs.OpenRoot(s.path(id, ""), false)
	if errors.Is(err, os.ErrNotExist) {
		return marker, false, nil
	}
	if err != nil {
		return marker, false, storageErrorAt("open_launch_marker_directory", err)
	}
	defer root.Close()
	body, err := root.ReadFile("launch.json", 4096)
	if errors.Is(err, os.ErrNotExist) {
		return marker, false, nil
	}
	if err != nil || json.Unmarshal(body, &marker) != nil || !validLaunchMarker(marker) {
		return marker, false, storageErrorAt("read_launch_marker", err)
	}
	return marker, true, nil
}
func (s Store) prepareLaunch(record Record, snapshots ...*network.ExecutionSnapshot) (*lockedLease, *protocol.Error) {
	if !validID(record.ID) {
		return nil, storageErrorAt("validate_launch_id", nil)
	}
	root, err := platformfs.OpenRoot(s.Data, true)
	if err != nil {
		return nil, storageErrorAt("open_launch_data", err)
	}
	defer root.Close()
	if err = root.MkdirAll("processes"); err != nil {
		return nil, storageErrorAt("create_process_root", err)
	}
	if err = root.MkdirAll("process-leases"); err != nil {
		return nil, storageErrorAt("create_lease_root", err)
	}
	lease, acquired, err := tryLease(s.leasePath(record.ID), true)
	if err != nil || !acquired {
		return nil, storageErrorAt("acquire_launch_lease", err)
	}
	keepLease := false
	defer func() {
		if !keepLease {
			_ = lease.unlockClose()
		}
	}()
	staging := filepath.Join("processes", launchStagingName(record.ID)+strings.ReplaceAll(tasks.ID(), "-", ""))
	if err = root.MkdirAll(staging); err != nil {
		return nil, storageErrorAt("create_launch_staging", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(filepath.Join(s.Data, staging))
		}
	}()
	write := func(name string, value any) error {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return root.WriteFile(context.Background(), filepath.Join(staging, name), body, false)
	}
	if err = write("record.json", record); err != nil {
		return nil, storageErrorAt("stage_launch_record", err)
	}
	if record.ProxyMode {
		if len(snapshots) == 0 || snapshots[0] == nil || snapshots[0].ID != record.NetworkSnapshotID {
			return nil, failure("network_snapshot_required")
		}
		if err = write("network.json", snapshots[0]); err != nil {
			return nil, storageErrorAt("stage_network_snapshot", err)
		}
	}
	if err = write("launch.json", platformLaunchMarker()); err != nil {
		return nil, storageErrorAt("stage_launch_marker", err)
	}
	if err = root.PublishDirectory(context.Background(), staging, filepath.Join("processes", record.ID)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, failure("request_conflict")
		}
		return nil, storageErrorAt("publish_launch_directory", err)
	}
	published = true
	if err = validateLeaseIdentity(lease.file, s.leasePath(record.ID)); err != nil {
		return nil, storageErrorAt("open_published_lease", err)
	}
	keepLease = true
	return lease, nil
}

func (s Store) cleanupLaunchStaging() *protocol.Error {
	root, err := platformfs.OpenRoot(s.root(), false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storageErrorAt("open_cleanup_root", err)
	}
	defer root.Close()
	entries, err := os.ReadDir(s.root())
	if err != nil {
		return storageErrorAt("enumerate_launch_staging", err)
	}
	for _, entry := range entries {
		if !validLaunchStagingName(entry.Name()) {
			continue
		}
		path := filepath.Join(s.root(), entry.Name())
		checked, err := platformfs.OpenRoot(path, false)
		if err != nil {
			return storageErrorAt("open_launch_staging", err)
		}
		if err = checked.Close(); err != nil {
			return storageErrorAt("close_launch_staging", err)
		}
		// Only unpublished stages under the pinned private process root are removed.
		// Stable process-leases is outside this tree and is never moved or deleted.
		// A crash may leave a stage for the next cleanup; no directory fsync is claimed.
		if err = os.RemoveAll(path); err != nil {
			return storageErrorAt("remove_launch_staging", err)
		}
	}
	return nil
}
