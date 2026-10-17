//go:build !windows

package tasks

import (
	"errors"
	"os"
)

func checkpointV3(resolution v3Resolution, state *State) error {
	info, err := os.Stat(resolution.WAL)
	if err != nil {
		return err
	}
	walSize := info.Size()
	needsCheckpoint := false
	var snapshot materializedState
	basis := state.checkpoint
	if basis == nil || basis.root != resolution.Root {
		if existing, existingState, err := readMaterializedSnapshot(resolution.Snapshot, resolution.Marker.Profile); err == nil {
			basis = &checkpointBasis{root: resolution.Root, walOffset: existing.WALOffset,
				lastEventAt: existing.LastEventAt, historyComplete: existingState.historyComplete}
		}
	}
	if basis != nil {
		snapshot.WALOffset, snapshot.LastEventAt = basis.walOffset, basis.lastEventAt
		if !basis.historyComplete {
			needsCheckpoint = true
		}
		if snapshot.WALOffset < 0 || snapshot.WALOffset > walSize {
			// Snapshot is an accelerator. The mutation path has already loaded
			// and validated the active WAL, so replace an impossible offset.
			needsCheckpoint = true
		} else {
			scan, scanErr := scanWAL(resolution.WAL, snapshot.WALOffset, true)
			switch {
			case scanErr != nil:
				// A stale/corrupt offset may point into a frame. Repair from the
				// current materialized state instead of making the accelerator
				// a permanent storage blocker.
				needsCheckpoint = true
			case scan.IncompleteTail:
				return errors.New("cannot checkpoint incomplete WAL")
			case len(scan.Frames) >= CheckpointMaxFrames || walSize-snapshot.WALOffset >= CheckpointMaxTailBytes:
				needsCheckpoint = true
			}
		}
	} else {
		// Snapshot is an accelerator; a valid WAL/state can repair it.
		needsCheckpoint = true
	}
	if !needsCheckpoint {
		return nil
	}
	lastEventAt := snapshot.LastEventAt
	if len(state.Events) > 0 {
		lastEventAt = state.Events[len(state.Events)-1].At
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState(resolution.Marker.Profile, state, walSize, lastEventAt)); err != nil {
		return err
	}
	if err := syncPrivateDirectory(resolution.Root); err != nil {
		return err
	}
	state.checkpoint = &checkpointBasis{root: resolution.Root, walOffset: walSize,
		lastEventAt: lastEventAt, historyComplete: state.historyComplete}
	return nil
}
