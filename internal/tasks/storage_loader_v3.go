package tasks

import (
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type taskStorageInfo struct {
	Profile profilekey.Resolution
	V3      *v3Resolution
}

func logicalVersionFromEvents(events []Event) int {
	version := 1
	for _, event := range events {
		if event.Action == "profile.upgraded" {
			version = JournalVersion
		}
	}
	return version
}

func (s Store) loadV3(resolution v3Resolution) (*Journal, *State, *protocol.Error) {
	scan, err := scanWAL(resolution.WAL, 0, true)
	if err != nil {
		return nil, nil, storageError()
	}
	journal := s.emptyJournal()
	expectedRevision := 0
	for _, frame := range scan.Frames {
		if frame.Meta.PreviousRevision != expectedRevision {
			return nil, nil, storageError()
		}
		for _, event := range frame.Events {
			if safeApplyEventSequence(journal.Events, event) != nil {
				return nil, nil, storageError()
			}
			journal.Events = append(journal.Events, event)
		}
		if frame.Receipt != nil {
			payload, exists, err := readCommittedReceipt(resolution, frame.Receipt.Key)
			if err != nil || !exists || payload.PayloadDigest != frame.Receipt.PayloadDigest || payload.FrameDigest != frame.Digest || payload.RequestID != frame.Meta.RequestID || payload.Fingerprint != frame.Meta.Fingerprint {
				return nil, nil, storageError()
			}
			if _, duplicate := journal.Receipts[payload.RequestID]; duplicate {
				return nil, nil, storageError()
			}
			journal.Receipts[payload.RequestID] = Receipt{Fingerprint: payload.Fingerprint, ContextHash: payload.ContextHash, Result: copyObject(payload.Result)}
		}
		for _, ref := range frame.Contexts {
			payload, exists, err := readCommittedContext(resolution, ref.Key)
			if err != nil || !exists || payload.PayloadDigest != ref.PayloadDigest || payload.FrameDigest != frame.Digest {
				return nil, nil, storageError()
			}
			if existing, duplicate := journal.Contexts[payload.ContextHash]; duplicate && existing != payload.RunID {
				return nil, nil, storageError()
			}
			journal.Contexts[payload.ContextHash] = payload.RunID
		}
		expectedRevision = frame.Meta.FinalRevision
	}
	journal.Version = logicalVersionFromEvents(journal.Events)
	state, replayErr := replayJournal(journal)
	if replayErr != nil {
		return nil, nil, replayErr
	}
	if state.Revision != expectedRevision {
		return nil, nil, storageError()
	}
	return journal, state, nil
}

func safeApplyEventSequence(existing []Event, event Event) error {
	if event.Sequence != len(existing)+1 {
		return errors.New("invalid event sequence")
	}
	return nil
}

func (s Store) resolveStorage() (taskStorageInfo, *protocol.Error) {
	var info taskStorageInfo
	if !project.ValidProfile(s.Profile) {
		return info, failure("invalid_argument", "Invalid profile.")
	}
	stat, err := os.Lstat(s.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return info, nil
	}
	if err != nil || !stat.IsDir() || stat.Mode().Perm()&0077 != 0 {
		return info, storageError()
	}
	resolution, err := profilekey.Resolve(s.Directory, s.Profile, "tasks")
	if err != nil {
		return info, storageError()
	}
	info.Profile = resolution
	if resolution.Mode == profilekey.ModeCanonical {
		v3, isV3, err := resolveV3(s.Directory, s.Profile)
		if err != nil {
			return info, storageError()
		}
		if isV3 {
			info.V3 = &v3
		}
	}
	return info, nil
}
