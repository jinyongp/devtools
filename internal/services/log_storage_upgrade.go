package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func LogStorageUpgradeAdapter() storageupgrade.Adapter {
	return storageupgrade.Adapter{Name: "process_logs", InspectHeld: func(ctx context.Context, root string) ([]storageupgrade.Step, error) {
		out := []storageupgrade.Step{}
		directory := Directory(root)
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !validID(entry.Name()) {
				continue
			}
			step := storageupgrade.Step{Domain: "process_logs", Resource: entry.Name(), Source: "unknown", Target: "prepared", State: "current", Fingerprint: entry.Name()}
			if err := ctx.Err(); err != nil {
				return append(out, step.InspectionFailed(err)), err
			}
			path := filepath.Join(directory, entry.Name(), "output.log")
			if _, err := platformfs.StatPrivate(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return append(out, step.InspectionFailed(err)), err
			}
			step.Source = "prepared"
			if _, err := platformfs.StatPrivate(path + ".lock"); errors.Is(err, os.ErrNotExist) {
				step.Source, step.State, step.Reason = "missing", "migration_required", "query_lock_preparation"
			} else if err != nil {
				return append(out, step.InspectionFailed(err)), err
			}
			if err := ctx.Err(); err != nil {
				return append(out, step.InspectionFailed(err)), err
			}
			out = append(out, step)
		}
		return out, nil
	}, Apply: func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		if !validID(step.Resource) {
			return false, errors.New("invalid process lock target")
		}
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return false, err
		}
		defer release()
		path := filepath.Join(Directory(root), step.Resource, "output.log")
		if _, err := platformfs.StatPrivate(path + ".lock"); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		unlock, err := acquireLogLock(ctx, path, true)
		if err != nil {
			return false, err
		}
		unlock()
		return true, nil
	}}
}
