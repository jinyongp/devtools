package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
)

type LogicalSnapshot struct {
	Source string
	Data   []byte
	Digest string
}

type StagedSnapshot struct {
	Generation string
	Files      map[string][]byte
}

func canonicalSnapshot(data []byte, profile string) ([]byte, string, *Journal, *State, error) {
	journal, state, err := inspectSnapshot(data, profile)
	if err != nil {
		return nil, "", nil, nil, err
	}
	if err := canonicalizeJournalReceipts(journal, state); err != nil {
		return nil, "", nil, nil, err
	}
	body, err := json.Marshal(journal)
	if err != nil {
		return nil, "", nil, nil, err
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), journal, state, nil
}

// CanonicalizeExactSnapshot validates a logical task snapshot and returns the
// deterministic representation used for archive equality and exact restore.
func CanonicalizeExactSnapshot(data []byte, profile string) ([]byte, string, error) {
	body, digest, _, _, err := canonicalSnapshot(data, profile)
	return body, digest, err
}

// ExportSnapshotHeld returns a canonical logical Journal while the caller holds
// the data-root maintenance gate. Physical v2/v3 details do not cross this API.
func (s Store) ExportSnapshotHeld(limit int64) (LogicalSnapshot, error) {
	var out LogicalSnapshot
	if err := s.CollectOrphanGenerationsHeld(); err != nil {
		return out, err
	}
	info, resolveErr := s.resolveStorage()
	if resolveErr != nil {
		return out, errors.New(resolveErr.Message)
	}
	if info.Profile.Mode == profilekey.ModeNone {
		return out, nil
	}
	if info.V3 != nil {
		if err := prepareWALForMutation(*info.V3); err != nil {
			return out, err
		}
		if err := recoverPendingV3(*info.V3); err != nil {
			return out, err
		}
	}
	var journal *Journal
	if info.V3 != nil {
		var loadErr *protocol.Error
		journal, loadErr = s.loadJournalV3ForExport(*info.V3)
		if loadErr != nil {
			return out, errors.New(loadErr.Message)
		}
		if err := canonicalizeExportJournal(journal); err != nil {
			return out, err
		}
	} else {
		var state *State
		var loadErr *protocol.Error
		journal, state, _, loadErr = s.loadResolved()
		if loadErr != nil {
			return out, errors.New(loadErr.Message)
		}
		if err := canonicalizeJournalReceipts(journal, state); err != nil {
			return out, err
		}
	}
	body, err := json.Marshal(journal)
	if err != nil {
		return out, err
	}
	if int64(len(body)) > limit {
		return out, errors.New("task snapshot exceeds limit")
	}
	source := info.Profile.CanonicalPath
	if info.Profile.Mode == profilekey.ModeLegacy {
		source = info.Profile.LegacyPath
	}
	sum := sha256.Sum256(body)
	out.Source = source
	out.Data = body
	out.Digest = hex.EncodeToString(sum[:])
	return out, nil
}

func (s Store) publicationFiles(storage taskStorageInfo, resolution v3Resolution) (map[string][]byte, error) {
	marker, err := taskMarkerBytes(s.Profile)
	if err != nil {
		return nil, err
	}
	head, err := taskHeadBytes(s.Profile, resolution.Generation)
	if err != nil {
		return nil, err
	}
	identity, err := profilekey.IdentityBytes(s.Profile, "tasks")
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{
		profilekey.StateRelative("tasks", s.Profile):    marker,
		profilekey.IdentityRelative("tasks", s.Profile): identity,
		taskHeadRelative(s.Profile):                     head,
	}
	if storage.Profile.LegacyStatus != profilekey.LegacyUnaddressable {
		legacyMarker, err := profilekey.MarkerBytes(s.Profile)
		if err != nil {
			return nil, err
		}
		files[profilekey.LegacyRelative("tasks", s.Profile)] = legacyMarker
	}
	return files, nil
}

// DeletionFilesHeld returns the complete logical task-domain deletion set.
// Generation directories become unreachable at commit and are reclaimed by
// CollectOrphanGenerationsHeld after publication.
func (s Store) DeletionFilesHeld() (map[string][]byte, error) {
	info, err := s.resolveStorage()
	if err != nil {
		return nil, errors.New(err.Message)
	}
	files := map[string][]byte{}
	switch info.Profile.Mode {
	case profilekey.ModeNone:
		return files, nil
	case profilekey.ModeLegacy:
		files[profilekey.LegacyRelative("tasks", s.Profile)] = nil
	case profilekey.ModeCanonical:
		files[profilekey.StateRelative("tasks", s.Profile)] = nil
		files[profilekey.IdentityRelative("tasks", s.Profile)] = nil
		files[taskHeadRelative(s.Profile)] = nil
		if info.Profile.LegacyStatus == profilekey.LegacyExisting {
			files[profilekey.LegacyRelative("tasks", s.Profile)] = nil
		}
	default:
		return nil, errors.New("invalid task storage mode")
	}
	return files, nil
}

// SnapshotFilesHeld stages a normalized backup snapshot under the target
// profile and returns its publication set.
func (s Store) SnapshotFilesHeld(data []byte) (StagedSnapshot, error) {
	return s.StageExactSnapshotHeld(data)
}

func mergeTaskFiles(dst map[string][]byte, staged StagedSnapshot) {
	for path, body := range staged.Files {
		dst[path] = body
	}
}

// PublishStagedHeld is for task-owned callers that do not already have a larger
// cross-domain maintenance transaction.
func (s Store) PublishStagedHeld(staged StagedSnapshot) error {
	if len(staged.Files) == 0 {
		return errors.New("empty task publication")
	}
	if err := maintenance.Replace(maintenance.Root(s.Directory), staged.Files); err != nil {
		return err
	}
	// Publication is already committed. Orphan reclamation is post-commit
	// housekeeping; the next exclusive task entry retries it fail-closed.
	_ = s.CollectOrphanGenerationsHeld()
	return nil
}
