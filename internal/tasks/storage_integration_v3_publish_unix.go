//go:build !windows

package tasks

import (
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/profilekey"
)

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
	if err != nil || !platformfs.DirectoryPrivate(root, info) {
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
