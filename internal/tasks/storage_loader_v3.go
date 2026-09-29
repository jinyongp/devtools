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

func readFrameReceipt(resolution v3Resolution, frame walFrame) (receiptCoordination, bool, error) {
	var payload receiptCoordination
	if frame.Receipt == nil {
		return payload, false, nil
	}
	value, exists, err := readCommittedReceipt(resolution, frame.Receipt.Key)
	if err != nil {
		return payload, false, err
	}
	if !exists ||
		value.PayloadDigest != frame.Receipt.PayloadDigest ||
		value.FrameDigest != frame.Digest ||
		value.RequestID != frame.Meta.RequestID ||
		value.Fingerprint != frame.Meta.Fingerprint {
		return payload, false, errors.New("receipt/frame mismatch")
	}
	return value, true, nil
}

func frameNeedsDefinitionSignature(frame walFrame) bool {
	for _, event := range frame.Events {
		if eventNeedsDefinitionSignature(event) {
			return true
		}
	}
	return false
}

func (s Store) journalFromV3Scan(resolution v3Resolution, scan walScan) (*Journal, *protocol.Error) {
	journal := s.emptyJournal()
	expectedRevision := 0
	for _, frame := range scan.Frames {
		if frame.Meta.PreviousRevision != expectedRevision {
			return nil, storageError()
		}
		receipt, hasReceipt, receiptErr := readFrameReceipt(resolution, frame)
		if receiptErr != nil {
			return nil, storageError()
		}
		for _, event := range frame.Events {
			if safeApplyEventSequence(journal.Events, event) != nil {
				return nil, storageError()
			}
			journal.Events = append(journal.Events, event)
		}
		if hasReceipt {
			if _, duplicate := journal.Receipts[receipt.RequestID]; duplicate {
				return nil, storageError()
			}
			journal.Receipts[receipt.RequestID] = Receipt{Fingerprint: receipt.Fingerprint, ContextHash: receipt.ContextHash, Result: copyObject(receipt.Result)}
		}
		for _, ref := range frame.Contexts {
			payload, exists, err := readCommittedContext(resolution, ref.Key)
			if err != nil || !exists || payload.PayloadDigest != ref.PayloadDigest || payload.FrameDigest != frame.Digest {
				return nil, storageError()
			}
			if existing, duplicate := journal.Contexts[payload.ContextHash]; duplicate && existing != payload.RunID {
				return nil, storageError()
			}
			journal.Contexts[payload.ContextHash] = payload.RunID
		}
		expectedRevision = frame.Meta.FinalRevision
	}
	if len(journal.Events) != expectedRevision {
		return nil, storageError()
	}
	journal.Version = logicalVersionFromEvents(journal.Events)
	return journal, nil
}

func (s Store) loadJournalV3(resolution v3Resolution) (*Journal, *protocol.Error) {
	scan, err := scanWAL(resolution.WAL, 0, true)
	if err != nil {
		return nil, storageError()
	}
	return s.journalFromV3Scan(resolution, scan)
}

func (s Store) loadJournalV3ForExport(resolution v3Resolution) (*Journal, *protocol.Error) {
	scan, err := scanWAL(resolution.WAL, 0, true)
	if err != nil {
		return nil, storageError()
	}
	journal, journalErr := s.journalFromV3Scan(resolution, scan)
	if journalErr != nil {
		return nil, journalErr
	}
	snapshot, state, snapshotErr := readMaterializedSnapshot(resolution.Snapshot, resolution.Marker.Profile)
	if snapshotErr == nil {
		tail := make([]walFrame, 0, len(scan.Frames))
		validOffset := true
		for _, frame := range scan.Frames {
			switch {
			case frame.EndOffset <= snapshot.WALOffset:
			case frame.StartOffset >= snapshot.WALOffset:
				tail = append(tail, frame)
			default:
				validOffset = false
			}
		}
		if validOffset {
			current, revision, applyErr := applyCurrentFrames(resolution, state, tail, snapshot.Revision)
			if applyErr == nil && current.Revision == revision && revision == len(journal.Events) && !(current.Version == 1 && current.Revision > 0) {
				return journal, nil
			}
		}
	}
	state, replayErr := replayJournal(journal)
	if replayErr != nil || state.Revision != len(journal.Events) {
		return nil, storageError()
	}
	return journal, nil
}

func (s Store) loadV3(resolution v3Resolution) (*Journal, *State, *protocol.Error) {
	journal, err := s.loadJournalV3(resolution)
	if err != nil {
		return nil, nil, err
	}
	state, replayErr := replayJournal(journal)
	if replayErr != nil {
		return nil, nil, replayErr
	}
	if state.Revision != len(journal.Events) {
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
