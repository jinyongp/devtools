package valuehandoff

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func deliveryLockStep(ctx context.Context, root, relative string) (step storageupgrade.Step, err error) {
	step = storageupgrade.Step{Domain: "delivery_locks", Resource: relative, Source: "unknown", Target: "prepared", State: "current"}
	defer func() {
		if err == nil {
			err = ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return step, err
	}
	body, err := maintenance.Read(filepath.Join(root, "value-executions", relative), 16<<20)
	if err != nil {
		return step, err
	}
	step.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(body))
	lock := filepath.Join(root, "value-execution-locks", filepath.Dir(relative), strings.TrimSuffix(filepath.Base(relative), ".json")+".lock")
	info, err := platformfs.StatPrivate(lock)
	if errors.Is(err, os.ErrNotExist) {
		step.Source, step.State, step.Reason = "missing", "migration_required", "query_lock_preparation"
		return step, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return step, errors.New("invalid delivery lock")
	}
	step.Source = "prepared"
	return step, nil
}

func StorageUpgradeAdapter() storageupgrade.Adapter {
	return storageupgrade.Adapter{Name: "delivery_locks", InspectHeld: func(ctx context.Context, root string) ([]storageupgrade.Step, error) {
		out := []storageupgrade.Step{}
		dir := filepath.Join(root, "value-executions")
		profiles, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		for _, profile := range profiles {
			profileStep := storageupgrade.Step{Domain: "delivery_locks", Resource: profile.Name()}
			if err := ctx.Err(); err != nil {
				return append(out, profileStep.InspectionFailed(err)), err
			}
			entries, err := os.ReadDir(filepath.Join(dir, profile.Name()))
			if err != nil {
				return append(out, profileStep.InspectionFailed(err)), err
			}
			for _, entry := range entries {
				relative := filepath.Join(profile.Name(), entry.Name())
				if !strings.HasSuffix(entry.Name(), ".json") || !idRE.MatchString(strings.TrimSuffix(entry.Name(), ".json")) {
					err := errors.New("invalid delivery record address")
					return append(out, (storageupgrade.Step{Domain: "delivery_locks", Resource: relative}).InspectionFailed(err)), err
				}
				step, err := deliveryLockStep(ctx, root, relative)
				if err != nil {
					return append(out, step.InspectionFailed(err)), err
				}
				out = append(out, step)
			}
		}
		return out, nil
	}, Apply: func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return false, err
		}
		defer release()
		current, err := deliveryLockStep(ctx, root, step.Resource)
		if err != nil {
			return false, err
		}
		if current.State == "current" {
			return false, nil
		}
		if current.Fingerprint != step.Fingerprint {
			return false, errors.New("delivery record changed during preparation")
		}
		lock := filepath.Join(root, "value-execution-locks", filepath.Dir(step.Resource), strings.TrimSuffix(filepath.Base(step.Resource), ".json")+".lock")
		unlock, err := maintenance.LockExclusive(ctx, lock)
		if err != nil {
			return false, err
		}
		unlock()
		return true, nil
	}}
}
