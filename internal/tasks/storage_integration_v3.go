package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
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
	journal, state, _, loadErr := s.loadResolved()
	if loadErr != nil {
		return out, errors.New(loadErr.Message)
	}
	if err := canonicalizeJournalReceipts(journal, state); err != nil {
		return out, err
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

// StageExactSnapshotHeld writes a complete, unpublished v3 generation and
// returns only the small publication set that the caller should include in its
// existing maintenance.Replace transaction.
func (s Store) StageExactSnapshotHeld(data []byte) (StagedSnapshot, error) {
	var staged StagedSnapshot
	body, _, journal, state, err := canonicalSnapshot(data, s.Profile)
	if err != nil {
		return staged, err
	}
	_ = body
	if err := PrivateDir(s.Directory); err != nil {
		return staged, err
	}
	if err := s.CollectOrphanGenerationsHeld(); err != nil {
		return staged, err
	}
	storage, resolveErr := s.resolveStorage()
	if resolveErr != nil {
		return staged, errors.New(resolveErr.Message)
	}
	generation := ID()
	resolution, err := createGeneration(s.Directory, s.Profile, generation)
	if err != nil {
		return staged, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(resolution.Root)
			_ = syncPrivateDirectory(filepath.Dir(resolution.Root))
		}
	}()
	if err := s.historicalFrames(resolution, journal, len(journal.Events), "", ""); err != nil {
		return staged, err
	}
	info, err := os.Stat(resolution.WAL)
	if err != nil {
		return staged, err
	}
	lastEventAt := ""
	if len(journal.Events) > 0 {
		lastEventAt = journal.Events[len(journal.Events)-1].At
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState(s.Profile, state, info.Size(), lastEventAt)); err != nil {
		return staged, err
	}
	if err := generationDurable(resolution); err != nil {
		return staged, err
	}
	files, err := s.publicationFiles(storage, resolution)
	if err != nil {
		return staged, err
	}
	success = true
	staged.Generation = generation
	staged.Files = files
	return staged, nil
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

// CollectOrphanGenerationsHeld removes every task generation not referenced by
// the current v3 head. The caller must hold the data-root maintenance gate.
func (s Store) CollectOrphanGenerationsHeld() error {
	if _, err := os.Lstat(s.Directory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	active := ""
	profile, err := profilekey.Resolve(s.Directory, s.Profile, "tasks")
	if err != nil {
		return err
	}
	if profile.Mode == profilekey.ModeCanonical {
		if resolution, ok, err := resolveV3(s.Directory, s.Profile); err != nil {
			return err
		} else if ok {
			active = resolution.Generation
		}
	}
	root := taskGenerationRoot(s.Directory, s.Profile)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("invalid generation root")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == active {
			continue
		}
		if !validID(entry.Name()) {
			return errors.New("invalid generation entry")
		}
		path := filepath.Join(root, entry.Name())
		if err := validatePrivateDirectory(path); err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return syncPrivateDirectory(root)
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
