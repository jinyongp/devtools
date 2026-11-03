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

func deliveryLockStep(root, relative string) (storageupgrade.Step, error) {
	step := storageupgrade.Step{Domain: "delivery_locks", Resource: relative, Source: "prepared", Target: "prepared", State: "current"}
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
			entries, err := os.ReadDir(filepath.Join(dir, profile.Name()))
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if !strings.HasSuffix(entry.Name(), ".json") || !idRE.MatchString(strings.TrimSuffix(entry.Name(), ".json")) {
					return nil, errors.New("invalid delivery record address")
				}
				step, err := deliveryLockStep(root, filepath.Join(profile.Name(), entry.Name()))
				if err != nil {
					return nil, err
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
		current, err := deliveryLockStep(root, step.Resource)
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
