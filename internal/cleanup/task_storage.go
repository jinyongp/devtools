package cleanup

import (
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (e Engine) taskStore(profile string) tasks.Store {
	return tasks.Store{Directory: filepath.Join(e.Data, "tasks"), Profile: profile}
}

// taskSnapshot is used only while Engine.lock holds the maintenance gate.
func (e Engine) taskSnapshot(profile string) (tasks.LogicalSnapshot, error) {
	return e.taskStore(profile).ExportSnapshotHeld(128 << 20)
}

func (e Engine) retireCompletedTasks(c candidate, archive Archive, already bool) (Archive, *protocol.Error) {
	store := e.taskStore(c.Profile)
	snapshot, err := store.ExportSnapshotHeld(128 << 20)
	if err != nil {
		return archive, fail("storage_error")
	}
	if snapshot.Data == nil && already {
		_ = store.CollectOrphanGenerationsHeld()
		return archive, nil
	}
	if snapshot.Data == nil || snapshot.Source != c.Source || snapshot.Digest != c.Stamp {
		return archive, fail("revision_conflict")
	}
	if !already {
		if maintenance.Write(e.archivePath(c.ID, "payload"), snapshot.Data) != nil || write(e.archivePath(c.ID, "entry.json"), archive) != nil {
			return archive, fail("storage_error")
		}
	}
	files, err := store.DeletionFilesHeld()
	if err != nil || maintenance.Replace(e.Data, files) != nil {
		return archive, fail("storage_error")
	}
	// The logical deletion committed in maintenance.Replace. Reclamation is
	// post-commit cleanup and must not turn a committed archive into failure.
	_ = store.CollectOrphanGenerationsHeld()
	return archive, nil
}

func (e Engine) restoreCompletedTasks(archive Archive, payload []byte) *protocol.Error {
	store := e.taskStore(archive.Profile)
	canonical, archivedDigest, err := tasks.CanonicalizeExactSnapshot(payload, archive.Profile)
	if err != nil {
		return fail("storage_error")
	}
	current, err := store.ExportSnapshotHeld(128 << 20)
	if err != nil {
		return fail("storage_error")
	}
	if current.Data != nil {
		if current.Digest != archivedDigest {
			return fail("revision_conflict")
		}
		_ = store.CollectOrphanGenerationsHeld()
		return nil
	}
	staged, err := store.StageExactSnapshotHeld(canonical)
	if err != nil {
		return fail("storage_error")
	}
	if maintenance.Replace(e.Data, staged.Files) != nil {
		return fail("storage_error")
	}
	// Head publication is already committed; orphan deletion is retried by the
	// next caller-held operation if it cannot be completed now.
	_ = store.CollectOrphanGenerationsHeld()
	return nil
}
