package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func singletonIdentity(profile, directory, command string) string {
	b, _ := json.Marshal([]string{profile, directory, command})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// Existing executions supply immutable identity even after configuration changes.
// A new start uses the same canonical root identity as instance registration.
func (s Store) operationIdentity(q Request) (string, *protocol.Error) {
	id := q.ID
	if q.Action == "start" {
		id = q.RequestID
	} else if q.Action != "stop" && q.Action != "restart" {
		return "", failure("invalid_argument")
	}
	if r, e := s.read(id); e == nil {
		return singletonIdentity(r.Profile, r.Directory, r.Command), nil
	} else if q.Action != "start" || e.Code != "process_not_found" {
		return "", e
	}
	p, e := project.Resolve(q.Directory, "")
	if e != nil {
		return "", e
	}
	return singletonIdentity(p.Profile, p.Root, q.Command), nil
}

func (s Store) singletonRecords(ctx context.Context, profile, directory, command string) ([]Record, *protocol.Error) {
	stored, e := s.storedRecords()
	if e != nil {
		return nil, e
	}
	items := []Record{}
	for _, r := range stored {
		if r.Profile != profile || r.Directory != directory || r.Command != command {
			continue
		}
		live, e := s.statusRecord(ctx, r)
		if e != nil {
			return nil, e
		}
		items = append(items, live)
	}
	return items, nil
}

func (s Store) publicationLock(ctx context.Context) (func(), *protocol.Error) {
	release, e := tasks.Lock(ctx, filepath.Join(s.root(), "publication.lock"))
	if e != nil {
		return nil, storageErrorAt("lock_launch_publication", e)
	}
	return release, nil
}

func (s Store) prepareLaunchContext(ctx context.Context, r Record, snapshots ...*network.ExecutionSnapshot) (*lockedLease, *protocol.Error) {
	release, e := s.publicationLock(ctx)
	if e != nil {
		return nil, e
	}
	defer release()
	return s.prepareLaunchUnlocked(r, snapshots...)
}

func (s Store) prepareLaunch(r Record, snapshots ...*network.ExecutionSnapshot) (*lockedLease, *protocol.Error) {
	return s.prepareLaunchContext(context.Background(), r, snapshots...)
}

func (s Store) cleanupLaunchStagingContext(ctx context.Context) *protocol.Error {
	release, e := s.publicationLock(ctx)
	if e != nil {
		return e
	}
	defer release()
	return s.cleanupLaunchStagingUnlocked()
}

func (s Store) cleanupLaunchStaging() *protocol.Error {
	return s.cleanupLaunchStagingContext(context.Background())
}
