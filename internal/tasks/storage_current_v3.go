package tasks

import (
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
)

func applyCurrentFrames(state *State, frames []walFrame, expectedRevision int) (*State, int, error) {
	for _, frame := range frames {
		if frame.Meta.PreviousRevision != expectedRevision {
			return nil, expectedRevision, errors.New("WAL revision discontinuity")
		}
		for _, event := range frame.Events {
			if event.Sequence != state.Revision+1 {
				return nil, expectedRevision, errors.New("WAL event sequence mismatch")
			}
			if err := safeApply(state, event); err != nil {
				return nil, expectedRevision, err
			}
		}
		if state.Revision != frame.Meta.FinalRevision {
			return nil, expectedRevision, errors.New("WAL final revision mismatch")
		}
		expectedRevision = frame.Meta.FinalRevision
	}
	return state, expectedRevision, nil
}

func loadCurrentFromWAL(resolution v3Resolution) (*State, error) {
	scan, err := scanWAL(resolution.WAL, 0, true)
	if err != nil {
		return nil, err
	}
	state := NewState()
	current, revision, err := applyCurrentFrames(state, scan.Frames, 0)
	if err != nil {
		return nil, err
	}
	if current.Revision != revision {
		return nil, errors.New("current WAL revision mismatch")
	}
	return current, nil
}

func loadCurrentV3Cached(resolution v3Resolution) (*State, bool, error) {
	snapshot, state, snapshotErr := readMaterializedSnapshot(resolution.Snapshot, resolution.Marker.Profile)
	if snapshotErr == nil {
		scan, scanErr := scanWAL(resolution.WAL, snapshot.WALOffset, true)
		if scanErr == nil {
			current, revision, applyErr := applyCurrentFrames(state, scan.Frames, snapshot.Revision)
			if applyErr == nil && current.Revision == revision {
				needsCheckpoint := scan.IncompleteTail ||
					len(scan.Frames) >= CheckpointMaxFrames ||
					scan.ValidOffset-snapshot.WALOffset >= CheckpointMaxTailBytes
				// Version 1 assessments derive their upgrade baseline from full
				// history. Keep the rare restored-v1 path correct by replaying
				// the WAL from zero until the first real upgrade mutation.
				if current.Version == 1 && current.Revision > 0 {
					full, err := loadCurrentFromWAL(resolution)
					return full, needsCheckpoint, err
				}
				return current, needsCheckpoint, nil
			}
		}
	}
	// The snapshot is an accelerator. A missing/corrupt/stale snapshot does not
	// make a valid WAL unreadable.
	current, err := loadCurrentFromWAL(resolution)
	return current, true, err
}

func loadCurrentV3ForMutation(resolution v3Resolution) (*State, bool, error) {
	current, needsCheckpoint, err := loadCurrentV3Cached(resolution)
	if err != nil || current == nil || current.historyComplete {
		return current, needsCheckpoint, err
	}
	full, replayErr := loadCurrentFromWAL(resolution)
	return full, true, replayErr
}

func loadCurrentV3(resolution v3Resolution) (*State, error) {
	current, _, err := loadCurrentV3Cached(resolution)
	return current, err
}

func (s Store) loadCurrentResolved() (*State, taskStorageInfo, *protocol.Error) {
	info, resolveErr := s.resolveStorage()
	if resolveErr != nil {
		return nil, info, resolveErr
	}
	if info.V3 != nil {
		state, err := loadCurrentV3(*info.V3)
		if err != nil {
			return nil, info, storageError()
		}
		return state, info, nil
	}
	switch info.Profile.Mode {
	case profilekey.ModeNone:
		return NewState(), info, nil
	case profilekey.ModeLegacy:
		_, state, err := s.loadPath(info.Profile.LegacyPath)
		return state, info, err
	case profilekey.ModeCanonical:
		_, state, err := s.loadPath(info.Profile.CanonicalPath)
		return state, info, err
	default:
		return nil, info, storageError()
	}
}

func (s Store) loadCurrent() (*State, *protocol.Error) {
	state, _, err := s.loadCurrentResolved()
	return state, err
}

func sameTaskStorageResolution(left, right taskStorageInfo) bool {
	if left.Profile.Mode != right.Profile.Mode ||
		left.Profile.CanonicalPath != right.Profile.CanonicalPath ||
		left.Profile.LegacyPath != right.Profile.LegacyPath ||
		left.Profile.IdentityPath != right.Profile.IdentityPath ||
		left.Profile.LegacyStatus != right.Profile.LegacyStatus ||
		left.Profile.Marker != right.Profile.Marker {
		return false
	}
	if left.V3 == nil || right.V3 == nil {
		return left.V3 == nil && right.V3 == nil
	}
	return left.V3.Generation == right.V3.Generation
}

func (s Store) completionState() (*State, *protocol.Error) {
	for attempt := 0; attempt < 2; attempt++ {
		before, beforeErr := s.resolveStorage()
		if beforeErr != nil {
			continue
		}
		var state *State
		var readErr *protocol.Error
		if before.V3 != nil {
			current, err := loadCurrentV3(*before.V3)
			if err != nil {
				continue
			}
			state = current
		} else {
			switch before.Profile.Mode {
			case profilekey.ModeNone:
				state = NewState()
			case profilekey.ModeLegacy:
				_, state, readErr = s.loadPath(before.Profile.LegacyPath)
			case profilekey.ModeCanonical:
				_, state, readErr = s.loadPath(before.Profile.CanonicalPath)
			default:
				readErr = storageError()
			}
			if readErr != nil {
				continue
			}
		}
		if s.completionHook != nil {
			s.completionHook()
		}
		after, afterErr := s.resolveStorage()
		if afterErr == nil && sameTaskStorageResolution(before, after) {
			return state, nil
		}
	}
	if _, err := os.Lstat(s.Directory); errors.Is(err, os.ErrNotExist) {
		return NewState(), nil
	}
	return nil, storageError()
}

func loadHistoryV3(resolution v3Resolution) (*State, error) {
	scan, err := scanWAL(resolution.WAL, 0, true)
	if err != nil {
		return nil, err
	}
	state := NewState()
	expectedRevision := 0
	for _, frame := range scan.Frames {
		if frame.Meta.PreviousRevision != expectedRevision {
			return nil, errors.New("WAL revision discontinuity")
		}
		for _, event := range frame.Events {
			if event.Sequence != state.Revision+1 {
				return nil, errors.New("WAL event sequence mismatch")
			}
			if err := safeApply(state, event); err != nil {
				return nil, err
			}
		}
		if state.Revision != frame.Meta.FinalRevision {
			return nil, errors.New("WAL final revision mismatch")
		}
		expectedRevision = frame.Meta.FinalRevision
	}
	if state.Revision != expectedRevision {
		return nil, errors.New("history WAL revision mismatch")
	}
	return state, nil
}

func (s Store) loadHistory() (*State, *protocol.Error) {
	info, resolveErr := s.resolveStorage()
	if resolveErr != nil {
		return nil, resolveErr
	}
	if info.V3 != nil {
		state, err := loadHistoryV3(*info.V3)
		if err != nil {
			return nil, storageError()
		}
		return state, nil
	}
	switch info.Profile.Mode {
	case profilekey.ModeNone:
		return NewState(), nil
	case profilekey.ModeLegacy:
		_, state, err := s.loadPath(info.Profile.LegacyPath)
		return state, err
	case profilekey.ModeCanonical:
		_, state, err := s.loadPath(info.Profile.CanonicalPath)
		return state, err
	default:
		return nil, storageError()
	}
}
