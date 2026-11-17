package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

const completionIndexCompactionSize = 256

type completionIndex struct {
	Version int      `json:"version"`
	Ended   []string `json:"ended"`
}

func (s Store) completionIndexRoot() string { return filepath.Join(s.root(), ".completion") }

func (s Store) readCompletionIndex() (completionIndex, bool) {
	var index completionIndex
	err := tasks.ReadPrivate(filepath.Join(s.completionIndexRoot(), "ended.json"), &index)
	return index, err == nil && index.Version == 1
}

// A recorded terminal ID cannot be used for a new execution. Launch publication
// rejects an existing ID and cleanup restores only terminal execution records.
// The index supplies negative hints for Tab, never authority to act on an ID.
func (s Store) rememberCompletionEnd(id string) {
	changes := filepath.Join(s.completionIndexRoot(), "changes")
	path := filepath.Join(changes, id)
	if err := os.Mkdir(path, 0700); errors.Is(err, os.ErrNotExist) && tasks.PrivateDir(changes) == nil {
		_ = os.Mkdir(path, 0700)
	}
}

func (s Store) PrepareCompletionIndex(ctx context.Context) *protocol.Error {
	records, condition := s.navigationRecordsContext(ctx)
	if condition != nil {
		return condition
	}
	return s.publishCompletionIndex(ctx, records, true)
}

// MaintainCompletionIndex reuses metadata already read by retention maintenance.
func (s Store) MaintainCompletionIndex(ctx context.Context, records []Record) {
	_ = s.publishCompletionIndex(ctx, records, false)
}

func (s Store) publishCompletionIndex(ctx context.Context, records []Record, rebuild bool) *protocol.Error {
	if len(records) == 0 {
		return nil
	}
	root := s.completionIndexRoot()
	changes := filepath.Join(root, "changes")
	if !rebuild {
		if _, ready := s.readCompletionIndex(); ready {
			pending, err := os.ReadDir(changes)
			if err == nil && len(pending) < completionIndexCompactionSize {
				return nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return protocol.ContextError(err)
	}
	if err := tasks.PrivateDir(changes); err != nil {
		return storageErrorAt("create_completion_index", err)
	}
	unlock, err := tasks.Lock(ctx, filepath.Join(root, "publish.lock"))
	if err != nil {
		return storageErrorAt("lock_completion_index", err)
	}
	defer unlock()
	ended := make([]string, 0, len(records))
	for _, record := range records {
		if record.EndedAt != nil {
			ended = append(ended, record.ID)
		}
	}
	slices.Sort(ended)
	if err := tasks.WritePrivate(filepath.Join(root, "ended.json"), completionIndex{Version: 1, Ended: ended}); err != nil {
		return storageErrorAt("publish_completion_index", err)
	}
	pending, _ := os.ReadDir(changes)
	for _, entry := range pending {
		// A controller may have ended after the metadata scan. Its marker must
		// remain unless the new snapshot already contains that execution.
		if _, present := slices.BinarySearch(ended, entry.Name()); present {
			_ = os.Remove(filepath.Join(changes, entry.Name()))
		}
	}
	return nil
}

func (s Store) completionMetadata(ctx context.Context, selection CompletionSelection) ([]Record, *protocol.Error) {
	names, err := s.executionNames()
	if err != nil {
		return nil, storageErrorAt("enumerate_execution_ids", err)
	}
	var ended map[string]struct{}
	switch selection.Action {
	case "stop", "check", "wait":
		selection.ActiveOnly = true
	}
	if selection.ActiveOnly {
		ended = map[string]struct{}{}
		if index, ready := s.readCompletionIndex(); ready {
			ended = make(map[string]struct{}, len(index.Ended))
			for _, id := range index.Ended {
				ended[id] = struct{}{}
			}
		}
		changes, _ := os.ReadDir(filepath.Join(s.completionIndexRoot(), "changes"))
		for _, change := range changes {
			if change.IsDir() {
				ended[change.Name()] = struct{}{}
			}
		}
	}
	var records []Record
	for _, id := range names {
		if ctx.Err() != nil {
			break
		}
		if _, skip := ended[id]; skip || !idref.Matches(id, selection.Prefix) || !validID(id) {
			continue
		}
		record, err := readDiskRecord(s.path(id, "record.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !validRecord(id, record.Metadata) {
			return nil, storageErrorAt("read_execution_metadata", err)
		}
		if record.RetiredAt == nil && (!selection.ActiveOnly || record.Metadata.EndedAt == nil) {
			records = append(records, record.Metadata)
		}
	}
	return records, nil
}
