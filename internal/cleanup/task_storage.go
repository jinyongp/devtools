package cleanup

import (
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// taskSnapshot is used only while Engine.lock holds the maintenance gate.
func (e Engine) taskSnapshot(profile string) (string, []byte, error) {
	directory := filepath.Join(e.Data, "tasks")
	body, err := profilekey.Snapshot(directory, profile, "tasks", 128<<20)
	if err != nil || body == nil {
		return "", body, err
	}
	resolved, err := profilekey.Resolve(directory, profile, "tasks")
	if err != nil {
		return "", nil, err
	}
	path := resolved.CanonicalPath
	if resolved.Mode == profilekey.ModeLegacy {
		path = resolved.LegacyPath
	}
	return path, body, nil
}

func (e Engine) retireCompletedTasks(c candidate, archive Archive, already bool) (Archive, *protocol.Error) {
	path, body, err := e.taskSnapshot(c.Profile)
	if err != nil {
		return archive, fail("storage_error")
	}
	if body == nil && already {
		return archive, nil
	}
	if body == nil || path != c.Source || digest(body) != c.Stamp {
		return archive, fail("revision_conflict")
	}
	if !already {
		if maintenance.Write(e.archivePath(c.ID, "payload"), body) != nil || write(e.archivePath(c.ID, "entry.json"), archive) != nil {
			return archive, fail("storage_error")
		}
	}
	files, err := profilekey.Replacement(filepath.Join(e.Data, "tasks"), c.Profile, "tasks", nil)
	if err != nil || maintenance.Replace(e.Data, files) != nil {
		return archive, fail("storage_error")
	}
	return archive, nil
}

func (e Engine) restoreCompletedTasks(archive Archive, payload []byte) *protocol.Error {
	if _, err := tasks.InspectSnapshot(payload, archive.Profile); err != nil {
		return fail("storage_error")
	}
	_, body, err := e.taskSnapshot(archive.Profile)
	if err != nil {
		return fail("storage_error")
	}
	if body != nil {
		if digest(body) != digest(payload) {
			return fail("revision_conflict")
		}
		return nil
	}
	files, err := profilekey.Replacement(filepath.Join(e.Data, "tasks"), archive.Profile, "tasks", payload)
	if err != nil || maintenance.Replace(e.Data, files) != nil {
		return fail("storage_error")
	}
	return nil
}
