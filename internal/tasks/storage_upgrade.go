package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageerr"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func StorageUpgradeAdapter() storageupgrade.Adapter {
	adapter := storageupgrade.Layout("tasks")
	inspect := adapter.InspectHeld
	adapter.InspectHeld = func(ctx context.Context, root string) ([]storageupgrade.Step, error) {
		steps, err := inspect(ctx, root)
		if err != nil {
			return nil, err
		}
		for index := range steps {
			step := &steps[index]
			if err := ctx.Err(); err != nil {
				*step = step.InspectionFailed(err)
				return steps[:index+1], err
			}
			if step.State != "current" {
				continue
			}
			store := Store{Directory: filepath.Join(root, "tasks"), Profile: step.Profile}
			if err := store.inspectUpgrade(ctx, step); err != nil {
				return steps[:index+1], err
			}
		}
		return steps, nil
	}
	adapter.Apply = func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		store := Store{Directory: filepath.Join(root, "tasks"), Profile: step.Profile}
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return false, err
		}
		defer release()
		profileRelease, err := LockExclusive(ctx, store.path()+".lock")
		if err != nil {
			return false, err
		}
		defer profileRelease()
		current, err := storageupgrade.ProfileLayout(store.Directory, step.Profile, "tasks")
		if err != nil {
			return false, err
		}
		if current.State == "current" {
			return false, nil
		}
		if current.Fingerprint != step.Fingerprint {
			return false, storageupgrade.Required("tasks")
		}
		journal, state, storage, condition := store.loadMigrationResolved()
		if condition != nil {
			return false, condition
		}
		path := storage.Profile.CanonicalPath
		if storage.Profile.Mode == profilekey.ModeLegacy {
			path = storage.Profile.LegacyPath
		}
		raw, err := maintenance.Read(path, 128<<20)
		if err != nil {
			return false, err
		}
		defer clear(raw)
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return false, err
		}
		if err := storageupgrade.RetainRecoveryHeld(root, "tasks", step.Profile, map[string][]byte{filepath.ToSlash(relative): raw}); err != nil {
			return false, err
		}
		if err := store.migrateToV3(journal, state, storage, len(journal.Events), "", ""); err != nil {
			return false, err
		}
		after, condition := store.loadCurrent()
		if condition != nil {
			return true, condition
		}
		resolution, ok, err := resolveV3(store.Directory, store.Profile)
		if err != nil || !ok {
			return true, corruptStorage("verify_upgrade", "task_store")
		}
		verified, condition := store.loadJournalV3ForExport(resolution)
		if condition != nil {
			return true, condition
		}
		beforeView, _ := json.Marshal([]any{state.Items, state.Runs, state.Tracking})
		afterView, _ := json.Marshal([]any{after.Items, after.Runs, after.Tracking})
		if state.Revision != after.Revision || state.Version != after.Version || !bytes.Equal(beforeView, afterView) || !reflect.DeepEqual(journal.Events, verified.Events) || !reflect.DeepEqual(journal.Contexts, verified.Contexts) {
			return true, corruptStorage("verify_upgrade", "task_store")
		}
		return true, nil
	}
	return adapter
}

func (s Store) inspectUpgrade(ctx context.Context, step *storageupgrade.Step) error {
	finish := progress.Begin(ctx, "Checking task storage for profile "+s.Profile)
	defer finish()
	release, err := maintenance.LockPassiveShared(ctx, s.path()+".lock")
	if err != nil {
		var preparation *maintenance.QueryPreparationError
		if errors.As(err, &preparation) {
			step.State, step.Reason = "migration_required", preparation.State
			return nil
		}
		*step = step.InspectionFailed(err)
		return err
	}
	defer release()
	_, condition := s.loadCurrent()
	if err := ctx.Err(); err != nil {
		*step = step.InspectionFailed(err)
		return err
	}
	if condition != nil {
		*step = step.InspectionFailed(condition)
	}
	return nil
}

// loadMigrationResolved is the only disk-journal codec entry for old task
// containers. Ordinary reads and mutations require a current v3 container.
func (s Store) loadMigrationResolved() (*Journal, *State, taskStorageInfo, *protocol.Error) {
	info, condition := s.resolveStorage()
	if condition != nil {
		return nil, nil, info, condition
	}
	if info.V3 != nil {
		journal, state, condition := s.loadV3(*info.V3)
		return journal, state, info, condition
	}
	path := info.Profile.CanonicalPath
	if info.Profile.Mode == profilekey.ModeLegacy {
		path = info.Profile.LegacyPath
	}
	journal, state, condition := s.loadPath(path)
	return journal, state, info, condition
}

func (s Store) loadPath(path string) (*Journal, *State, *protocol.Error) {
	journal := s.emptyJournal()
	if err := ReadPrivate(path, journal); err != nil {
		return nil, nil, storageErrorAt("read_journal", "task_journal", err)
	}
	if journal.Profile != s.Profile || journal.Receipts == nil || journal.Contexts == nil {
		return nil, nil, storageErrorAt("validate_journal", "task_journal", storageerr.ErrCorrupt)
	}
	state, condition := replayJournal(journal)
	return journal, state, condition
}
