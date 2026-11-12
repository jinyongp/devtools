package tasks

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type ProfileRenameVersion struct {
	Generation string
	Revision   int
}

func requestFingerprint(r Request, options Object) string {
	return hash(Object{"action": r.Action, "target": r.Target, "body": r.Body, "options": options})
}

// The profile flag selects a store address. A receipt from a verified older
// address may be replayed at its owner's current address with the same input.
func matchesRenameReceipt(r Request, originalResult Object, originalFingerprint, fingerprint, profile string, names profilekey.RenameNames) bool {
	if originalFingerprint == fingerprint {
		return true
	}
	original, _ := originalResult["profile"].(string)
	if original == "" || original == profile || names.CurrentName(original) != profile {
		return false
	}
	if requested := r.Options["profile"]; requested != "" && requested != profile {
		return false
	}
	options := fingerprintOptions(r.Options)
	options["profile"] = original
	if requestFingerprint(r, options) == originalFingerprint {
		return true
	}
	delete(options, "profile")
	return requestFingerprint(r, options) == originalFingerprint
}

// ProfileRenameVersionHeld reads only current state and existing locks.
func (s Store) ProfileRenameVersionHeld(ctx context.Context) (*ProfileRenameVersion, *protocol.Error) {
	info, err := s.resolveStorage()
	if err != nil || info.Profile.Mode == profilekey.ModeNone {
		return nil, err
	}
	release, gateErr := maintenance.LockPassiveShared(ctx, s.path()+".lock")
	if gateErr != nil {
		var preparation *maintenance.QueryPreparationError
		if !errors.As(gateErr, &preparation) || preparation.State != "initialization_required" {
			return nil, s.queryGateError(ctx, gateErr)
		}
		// Exact restore can publish a generation before the first profile
		// lock exists. Retry the read under the lock if a writer created it.
		version, condition := s.readProfileRenameVersionHeld()
		release, gateErr = maintenance.LockPassiveShared(ctx, s.path()+".lock")
		if errors.As(gateErr, &preparation) && preparation.State == "initialization_required" {
			return version, condition
		}
		if gateErr != nil {
			return nil, s.queryGateError(ctx, gateErr)
		}
	}
	defer release()
	return s.readProfileRenameVersionHeld()
}

func (s Store) readProfileRenameVersionHeld() (*ProfileRenameVersion, *protocol.Error) {
	state, resolved, err := s.loadCurrentResolved()
	if err != nil {
		return nil, err
	}
	if resolved.V3 == nil {
		return nil, nil
	}
	if err := requireNoRunningTasks(state); err != nil {
		return nil, err
	}
	return &ProfileRenameVersion{Generation: resolved.V3.Generation, Revision: state.Revision}, nil
}

// ReaddressSnapshot preserves the original task graph, events, contexts and
// receipts. Only the address in the validated journal header changes.
func ReaddressSnapshot(data []byte, source, target string) ([]byte, error) {
	if !project.ValidProfile(source) || !project.ValidProfile(target) {
		return nil, errors.New("invalid task profile address")
	}
	journal, state, err := inspectSnapshot(data, source)
	if err != nil {
		return nil, err
	}
	if condition := requireNoRunningTasks(state); condition != nil {
		return nil, condition
	}
	journal.Profile = target
	return json.Marshal(journal)
}

func requireNoRunningTasks(state *State) *protocol.Error {
	for _, run := range state.Runs {
		if run.State == "running" {
			return failure("task_run_active", "Release active task claims before renaming the profile.")
		}
	}
	return nil
}

// ProfileRenameFilesHeld stages the same journal at a new address. The caller
// publishes the returned files under the same exclusive maintenance gate.
func (s Store) ProfileRenameFilesHeld(target string, limit int64) (map[string][]byte, error) {
	destination := s
	destination.Profile = target
	info, err := destination.resolveStorage()
	if err != nil {
		return nil, err
	}
	if info.Profile.Mode != profilekey.ModeNone {
		return nil, failure("profile_exists", "The target already has a task store.")
	}
	snapshot, snapshotErr := s.ExportSnapshotHeld(limit)
	if snapshotErr != nil || snapshot.Data == nil {
		return nil, snapshotErr
	}
	body, snapshotErr := ReaddressSnapshot(snapshot.Data, s.Profile, target)
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	files, snapshotErr := s.DeletionFilesHeld()
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	staged, snapshotErr := destination.StageExactSnapshotHeld(body)
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	mergeTaskFiles(files, staged)
	return files, nil
}
