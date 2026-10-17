package services

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

const legacyStartupGrace = 10 * time.Second

type launchMarker struct {
	Version      int    `json:"version"`
	LeaseHandoff string `json:"lease_handoff"`
}

type lockedLease struct {
	file       *os.File
	closeGuard func() error
}

func (l *lockedLease) closeReference() error {
	if l == nil || l.file == nil {
		return nil
	}
	var err error
	if l.closeGuard != nil {
		err = l.closeGuard()
		l.closeGuard = nil
	} else {
		err = l.file.Close()
	}
	l.file = nil
	return err
}

func privateRegular(file *os.File) bool {
	return platformfs.CheckFile(file) == nil
}

func validLaunchMarker(marker launchMarker) bool {
	return marker == platformLaunchMarker()
}

func (s Store) leaseAvailable(ctx context.Context, id string, allowMissing bool) (bool, *protocol.Error) {
	if !validID(id) {
		return false, storageError()
	}
	if ctx.Err() != nil {
		return false, failure("canceled")
	}
	lease, acquired, err := tryLease(s.leasePath(id), false)
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
	if !validID(id) {
		return nil, false, storageError()
	}
	if ctx.Err() != nil {
		return nil, false, failure("canceled")
	}
	lease, acquired, err := tryLease(s.leasePath(id), allowCreate)
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
