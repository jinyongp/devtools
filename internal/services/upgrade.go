package services

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// SupersededExecutions observes old controllers by their authenticated local
// status endpoint. An unreadable controller with a held lease is unresolved.
// The caller holds the process operation lock.
func (s Store) SupersededExecutions(ctx context.Context) ([]Record, *protocol.Error) {
	entries, err := os.ReadDir(s.root())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, storageErrorAt("enumerate_upgrade_executions", err)
	}
	var old []Record
	for _, entry := range entries {
		id := entry.Name()
		if !validID(id) {
			continue
		}
		record, condition := s.upgradeRecord(ctx, id)
		if condition != nil {
			if condition.Code == "process_not_found" {
				continue
			}
			return nil, condition
		}
		if record.EndedAt != nil {
			continue
		}
		live, err := s.rpc(ctx, id, "status")
		if err != nil {
			if !s.leaseFree(ctx, id) {
				return nil, cleanupUnknown(record)
			}
			continue
		}
		if live.controlBuild == s.Build || live.EndedAt != nil {
			continue
		}
		if live.State != "running" || live.Mode == "foreground" {
			return nil, protocol.NewError("process_pending", "Wait for the active execution to settle before applying the upgrade.", 3, map[string]any{
				"target":   map[string]any{"kind": "execution", "id": id, "name": record.Command},
				"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", "process", "status", id}, nil, "Inspect the execution before retrying the update.", "read").Remedy(protocol.InvocationContext{})},
			})
		}
		old = append(old, record)
	}
	return old, nil
}

func (s Store) upgradeRecord(ctx context.Context, id string) (Record, *protocol.Error) {
	r, e := s.readContext(ctx, id)
	if e == nil || e.Code != "migration_required" {
		return r, e
	}
	// Only the upgrade path can read the old format while its owner is still
	// present. The storage adapter remains responsible for conversion.
	if err := tasks.ReadPrivate(s.path(id, "record.json"), &r); err != nil || !validRecord(id, r) {
		return Record{}, storageErrorAt("read_previous_execution_record", err)
	}
	return r, nil
}

// StopForUpgrade asks only the recorded controller to stop. It never rewrites a
// record still owned by a previous executable. The process operation lock is
// held by the caller, preventing a new operation from changing the selection.
func (s Store) StopForUpgrade(ctx context.Context, id string) *protocol.Error {
	r, e := s.upgradeRecord(ctx, id)
	if e != nil {
		return e
	}
	if r.EndedAt == nil {
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
			closed, e := s.upgradeRecord(wait, id)
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
