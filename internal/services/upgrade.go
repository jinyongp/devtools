package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// SupersededExecutions observes old controllers by their authenticated local
// status endpoint. An unreadable controller with a held lease is unresolved.
// Callers recheck ownership under the existing operation gates before changes.
func (s Store) SupersededExecutions(ctx context.Context) ([]Record, *protocol.Error) {
	entries, err := os.ReadDir(s.root())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, storageErrorAt("enumerate_upgrade_executions", err)
	}
	var old []Record
	var pending *protocol.Error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return old, protocol.ContextError(ctx.Err())
		}
		id := entry.Name()
		if !validID(id) {
			continue
		}
		if disk, err := readDiskRecord(s.path(id, "record.json")); err == nil && validRecord(id, disk.Metadata) && disk.Metadata.EndedAt != nil {
			continue
		}
		record, _, condition := s.UpgradeRecord(ctx, id)
		if condition != nil {
			if condition.Code == "process_not_found" {
				continue
			}
			if pending == nil {
				pending = condition
			}
			continue
		}
		if record.EndedAt != nil || record.Mode == "foreground" {
			continue
		}
		live, err := s.rpc(ctx, id, "status")
		if err != nil {
			if !s.leaseFree(ctx, id) && pending == nil {
				pending = cleanupUnknown(record)
			}
			continue
		}
		if live.controlBuild == s.Build || live.EndedAt != nil || live.Mode == "foreground" {
			continue
		}
		if live.State != "running" {
			if pending == nil {
				pending = protocol.NewError("process_pending", "Wait for the active execution to settle before applying the upgrade.", 3, map[string]any{
					"target":   map[string]any{"kind": "execution", "id": id, "name": record.Command},
					"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", "process", "status", id}, nil, "Inspect the execution before retrying the update.", "read").Remedy(protocol.InvocationContext{})},
				})
			}
			continue
		}
		old = append(old, record)
	}
	return old, pending
}

func (s Store) UpgradeRecord(ctx context.Context, id string) (Record, bool, *protocol.Error) {
	r, e := s.readContext(ctx, id)
	if e == nil || e.Code != "migration_required" {
		return r, false, e
	}
	// Only the upgrade path can read the old format while its owner is still
	// present. The storage adapter remains responsible for conversion.
	if err := tasks.ReadPrivate(s.path(id, "record.json"), &r); err != nil || !validRecord(id, r) {
		return Record{}, false, storageErrorAt("read_previous_execution_record", err)
	}
	return r, true, nil
}

func (s Store) ConvertUpgradeRecord(ctx context.Context, id string) *protocol.Error {
	if !validID(id) {
		return failure("invalid_argument")
	}
	step, err := processStorageStep(ctx, s.Data, filepath.Join(id, "record.json"))
	if err != nil {
		return storageErrorAt("inspect_previous_execution_record", err)
	}
	if step.State == "current" {
		return nil
	}
	if step.State != "migration_required" {
		return failure("process_pending")
	}
	if _, err := StorageUpgradeAdapter().Apply(ctx, s.Data, step); err != nil {
		return storageErrorAt("upgrade_previous_execution_record", err)
	}
	return nil
}

func (s Store) UpgradeNeeded(ctx context.Context, id string) (bool, *protocol.Error) {
	live, err := s.rpc(ctx, id, "status")
	if err != nil || live.State != "running" || live.EndedAt != nil || live.Mode == "foreground" {
		return false, failure("process_pending")
	}
	return live.controlBuild != s.Build, nil
}

// StopForUpgrade asks only the recorded controller to stop. It never rewrites a
// record still owned by a previous executable. The process operation lock is
// held by the caller, preventing a new operation from changing the selection.
func (s Store) StopForUpgrade(ctx context.Context, id string) *protocol.Error {
	r, _, e := s.UpgradeRecord(ctx, id)
	if e != nil {
		return e
	}
	if r.EndedAt == nil {
		live, err := s.rpc(ctx, id, "status")
		if err != nil {
			return cleanupUnknown(r)
		}
		if r.Mode == "foreground" || live.Mode == "foreground" || live.controlBuild == s.Build || live.State != "running" {
			return failure("process_pending")
		}
		if _, err := s.rpc(ctx, id, "stop"); err != nil && !s.leaseFree(ctx, id) {
			return cleanupUnknown(r)
		}
	}
	wait, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.leaseFree(wait, id) {
			closed, _, e := s.UpgradeRecord(wait, id)
			if e != nil {
				return e
			}
			if closed.EndedAt == nil || unverifiedOwnedCleanup(closed) {
				return cleanupUnknown(closed)
			}
			return nil
		}
		select {
		case <-wait.Done():
			return failure("process_pending")
		case <-ticker.C:
		}
	}
}
