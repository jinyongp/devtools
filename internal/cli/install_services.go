package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/installstorage"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

type installedServiceUpgrade struct {
	Build   string                   `json:"build"`
	Targets []installedServiceTarget `json:"targets"`
}

type installedServiceTarget struct {
	Previous string `json:"previous"`
	Request  string `json:"request"`
	Stopped  bool   `json:"stopped,omitempty"`
	Restored string `json:"restored,omitempty"`
}

func (a *App) upgradeInstalledServices(ctx context.Context, streams IO, next func() installstorage.Result) installstorage.Result {
	store, e := a.serviceStore()
	if e != nil {
		return pendingInstallService(e)
	}
	if _, err := os.Lstat(filepath.Join(store.Data, "processes")); errors.Is(err, os.ErrNotExist) || a.build == "" {
		return next()
	}
	path := filepath.Join(store.Data, ".installed-executions-upgrade.json")
	release, err := tasks.Lock(ctx, filepath.Join(store.Data, ".installed-executions-upgrade.lock"))
	if err != nil {
		return pendingInstallService(protocol.NewError("process_pending", "Cannot acquire the execution upgrade lock.", 3, nil))
	}
	defer release()
	plan, e := a.stopInstalledServices(ctx, store, path)
	if e != nil {
		return pendingInstallService(e)
	}
	result := next()
	if result.Report == nil || !result.Report.Ready {
		return result
	}
	restored := make([]map[string]any, 0, len(plan.Targets))
	allRestored := true
	for i := range plan.Targets {
		target := &plan.Targets[i]
		if target.Restored == "" {
			target.Restored, e = a.restoreInstalledService(ctx, streams, store, *target)
			if e != nil {
				result.Ready, result.Status = false, "pending"
				result.Diagnostic = protocol.ObservationDiagnostic(e)
				allRestored = false
				continue
			}
			if err := tasks.WritePrivate(path, plan); err != nil {
				result.Ready, result.Status = false, "pending"
				result.Diagnostic = protocol.ObservationDiagnostic(installServiceStorageError())
				return result
			}
			result.RuntimeChanged = true
		} else {
			live, e := store.Status(ctx, target.Restored)
			if e != nil || live.State != "running" || live.EndedAt != nil {
				if e == nil {
					e = protocol.NewError("process_pending", "The restored execution is no longer running.", 3, map[string]any{"target": map[string]any{"kind": "execution", "id": target.Restored}})
				}
				result.Ready, result.Status = false, "pending"
				result.Diagnostic = protocol.ObservationDiagnostic(e)
				allRestored = false
				continue
			}
		}
		restored = append(restored, map[string]any{"previous_id": target.Previous, "execution_id": target.Restored})
		result.Processes = restored
	}
	if !allRestored {
		return result
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		result.Ready, result.Status = false, "pending"
		result.Diagnostic = protocol.ObservationDiagnostic(installServiceStorageError())
	}
	return result
}

func (a *App) stopInstalledServices(ctx context.Context, store services.Store, path string) (installedServiceUpgrade, *protocol.Error) {
	var plan installedServiceUpgrade
	release, err := store.Lock(ctx)
	if err != nil {
		return plan, protocol.NewError("process_pending", "Cannot acquire the process operation lock.", 3, nil)
	}
	defer release()
	err = tasks.ReadPrivate(path, &plan)
	if errors.Is(err, os.ErrNotExist) {
		records, e := store.SupersededExecutions(ctx)
		if e != nil {
			return plan, e
		}
		plan.Build = a.build
		for _, record := range records {
			project, e := a.resolveStoredProject(record.Directory, "")
			if e != nil {
				return plan, e
			}
			if _, ok := project.Commands[record.Command]; !ok {
				return plan, protocol.NewError("command_not_found", "A running service no longer has its configured command.", 3, map[string]any{"target": map[string]any{"kind": "execution", "id": record.ID, "name": record.Command}})
			}
			if project.Profile != record.Profile || project.Root != record.Directory {
				return plan, protocol.NewError("instance_conflict", "The running service belongs to a different project identity.", 3, map[string]any{"target": map[string]any{"kind": "execution", "id": record.ID, "name": record.Command}})
			}
			plan.Targets = append(plan.Targets, installedServiceTarget{Previous: record.ID, Request: tasks.ID()})
		}
		if len(plan.Targets) != 0 && tasks.WritePrivate(path, plan) != nil {
			return plan, installServiceStorageError()
		}
	} else if err != nil || plan.Build != a.build {
		return plan, installServiceStorageError()
	}
	for i := range plan.Targets {
		target := &plan.Targets[i]
		if target.Stopped {
			continue
		}
		if e := store.StopForUpgrade(ctx, target.Previous); e != nil {
			return plan, e
		}
		target.Stopped = true
		if err := tasks.WritePrivate(path, plan); err != nil {
			return plan, installServiceStorageError()
		}
	}
	return plan, nil
}

func (a *App) restoreInstalledService(ctx context.Context, streams IO, store services.Store, target installedServiceTarget) (string, *protocol.Error) {
	request := services.Request{Action: "restart", ID: target.Previous, RequestID: target.Request}
	result, replayed, e := store.RecoverCompleted(ctx, request, "", network.PrepareAuto)
	if e != nil {
		return "", e
	}
	if !replayed {
		original, e := store.Status(ctx, target.Previous)
		if e != nil {
			return "", e
		}
		if e := a.prepareServiceRestart(ctx, streams, &request, original); e != nil {
			return "", e
		}
		result, e = store.Apply(ctx, request)
		if e != nil {
			return "", e
		}
	}
	live, e := store.Status(ctx, result.Item.ID)
	if e != nil {
		return "", e
	}
	if live.ReadyConfigured && live.State == "running" {
		live, e = store.Wait(ctx, live.ID, 10*time.Second)
		if e != nil {
			return "", e
		}
	}
	if live.State != "running" || live.EndedAt != nil {
		return "", protocol.NewError("process_pending", "The replacement execution did not remain running.", 3, map[string]any{"target": map[string]any{"kind": "execution", "id": live.ID, "name": live.Command}})
	}
	return live.ID, nil
}

func installServiceStorageError() *protocol.Error {
	return protocol.NewError("process_upgrade_storage", "Cannot verify the pending execution upgrade for this installed build.", 3, nil)
}

func pendingInstallService(e *protocol.Error) installstorage.Result {
	return installstorage.Result{Status: "pending", Diagnostic: protocol.ObservationDiagnostic(e)}
}
