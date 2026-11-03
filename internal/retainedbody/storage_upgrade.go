package retainedbody

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func archiveStep(root, id string) (storageupgrade.Step, error) {
	step := storageupgrade.Step{Domain: "archives", Resource: id, Source: "archive-v2", Target: "archive-v2", State: "current"}
	raw, err := maintenance.Read(filepath.Join(root, "archives", id, "payload"), MaxBytes)
	if err != nil {
		return step, err
	}
	step.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(raw))
	var e Envelope
	if json.Unmarshal(raw, &e) == nil && e.Version != 0 {
		if e.Version != 2 || !e.Body.Valid() {
			return step, errors.New("unsupported archive payload")
		}
		_, err = (Store{Root: root}).Read(e.Body)
		return step, err
	}
	step.Source, step.State, step.Reason = "archive-v1", "migration_required", "shared_body"
	return step, nil
}

func StorageUpgradeAdapter() storageupgrade.Adapter {
	return storageupgrade.Adapter{Name: "archives", InspectHeld: func(ctx context.Context, root string) ([]storageupgrade.Step, error) {
		out := []storageupgrade.Step{}
		if _, err := os.Lstat(filepath.Join(root, "retained-bodies")); err == nil {
			release, err := (Store{Root: root}).Query(ctx)
			if err == nil {
				release()
			} else {
				var preparation *maintenance.QueryPreparationError
				if !errors.As(err, &preparation) {
					return nil, err
				}
				out = append(out, storageupgrade.Step{Domain: "archives", Resource: "@publication-lock", Source: "unprepared", Target: "prepared", State: "migration_required", Reason: "query_lock_preparation", Fingerprint: "retained-bodies-v1"})
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		entries, err := os.ReadDir(filepath.Join(root, "archives"))
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// An archive entry, not an arbitrary directory, owns this payload.
			var header struct {
				ID       string `json:"id"`
				PurgedAt any    `json:"purged_at"`
			}
			raw, err := maintenance.Read(filepath.Join(root, "archives", entry.Name(), "entry.json"), 1<<20)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if json.Unmarshal(raw, &header) != nil || header.ID != entry.Name() {
				return nil, errors.New("invalid archive entry")
			}
			if header.PurgedAt != nil {
				continue
			}
			step, err := archiveStep(root, entry.Name())
			if err != nil {
				return nil, err
			}
			out = append(out, step)
		}
		return out, nil
	}, Apply: func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		unlock, err := maintenance.LockExclusive(ctx, filepath.Join(root, "processes", "operations.lock"))
		if err != nil {
			return false, err
		}
		defer unlock()
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return false, err
		}
		defer release()
		if step.Resource == "@publication-lock" {
			unlock, err := (Store{Root: root}).Publication(ctx)
			if err != nil {
				return false, err
			}
			unlock()
			return true, nil
		}
		current, err := archiveStep(root, step.Resource)
		if err != nil {
			return false, err
		}
		if current.State == "current" {
			return false, nil
		}
		if current.Fingerprint != step.Fingerprint {
			return false, errors.New("archive changed during upgrade")
		}
		path := filepath.Join(root, "archives", step.Resource, "payload")
		raw, err := maintenance.Read(path, MaxBytes)
		if err != nil {
			return false, err
		}
		entry, err := maintenance.Read(filepath.Join(root, "archives", step.Resource, "entry.json"), 1<<20)
		if err != nil {
			return false, err
		}
		var header struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(entry, &header) != nil {
			return false, errors.New("invalid archive entry")
		}
		kind := "archive_payload"
		if header.Kind == "completed_process" {
			kind = "process_record"
		}
		err = (Store{Root: root}).WriteReference(ctx, path, kind, raw)
		return err == nil, err
	}}
}
