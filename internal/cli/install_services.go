package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

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
	Started  bool   `json:"started,omitempty"`
	Restored string `json:"restored,omitempty"`
}

func (a *App) upgradeInstalledServices(ctx context.Context, streams IO, networkReady bool) ([]map[string]any, *protocol.Error) {
	store, e := a.serviceStore()
	if e != nil {
		return nil, e
	}
	if _, err := os.Lstat(filepath.Join(store.Data, "processes")); errors.Is(err, os.ErrNotExist) || a.build == "" {
		return nil, nil
	}
	path := filepath.Join(store.Data, ".installed-executions-upgrade.json")
	release, err := tasks.Lock(ctx, filepath.Join(store.Data, ".installed-executions-upgrade.lock"))
	if err != nil {
		return nil, protocol.NewError("process_pending", "Cannot acquire the execution upgrade lock.", 3, nil)
	}
	defer release()
	plan, condition := a.planInstalledServices(ctx, store, path)
	restored := make([]map[string]any, 0, len(plan.Targets))
	remaining := make([]installedServiceTarget, 0, len(plan.Targets))
	followUp := []installedServiceTarget{}
	for i := range plan.Targets {
		target := &plan.Targets[i]
		if !target.Started && !target.Stopped && target.Restored == "" {
			original, _, e := store.UpgradeRecord(ctx, target.Previous)
			if e != nil {
				if condition == nil {
					condition = e
				}
				remaining = append(remaining, *target)
				continue
			}
			if original.EndedAt != nil {
				continue
			}
			target.Started = true
			if err := tasks.WritePrivate(path, plan); err != nil {
				return restored, installServiceStorageError()
			}
		}
		id, e := a.restoreInstalledService(ctx, streams, store, *target, networkReady)
		if e != nil {
			if e.Details == nil {
				e.Details = map[string]any{}
			}
			if e.Details["target"] == nil {
				identity := map[string]any{"kind": "execution", "id": target.Previous}
				if original, _, readErr := store.UpgradeRecord(ctx, target.Previous); readErr == nil {
					identity["name"], identity["profile"] = original.Command, original.Profile
				}
				e.Details["target"] = identity
			}
			if condition == nil {
				condition = e
			}
			remaining = append(remaining, *target)
			continue
		}
		target.Restored = id
		if err := tasks.WritePrivate(path, plan); err != nil {
			return restored, installServiceStorageError()
		}
		restored = append(restored, map[string]any{"previous_id": target.Previous, "execution_id": id})
		if plan.Build != a.build {
			needsUpgrade, e := store.UpgradeNeeded(ctx, id)
			if e != nil {
				if condition == nil {
					condition = e
				}
				remaining = append(remaining, *target)
				continue
			}
			if needsUpgrade {
				followUp = append(followUp, installedServiceTarget{Previous: id, Request: tasks.ID()})
			}
		}
	}
	remaining = append(remaining, followUp...)
	if len(remaining) > 0 {
		plan.Targets = remaining
		if err := tasks.WritePrivate(path, plan); err != nil {
			return restored, installServiceStorageError()
		}
		if condition == nil && len(followUp) > 0 {
			condition = protocol.NewError("process_pending", "Restored executions from a previous build are queued for replacement.", 3, nil)
		}
	} else if len(plan.Targets) > 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return restored, installServiceStorageError()
		}
	}
	return restored, condition
}

func (a *App) planInstalledServices(ctx context.Context, store services.Store, path string) (installedServiceUpgrade, *protocol.Error) {
	var plan installedServiceUpgrade
	err := tasks.ReadPrivate(path, &plan)
	if err == nil {
		seen := map[string]bool{}
		for _, target := range plan.Targets {
			if !protocol.ValidUUID(target.Previous) || !protocol.ValidUUID(target.Request) || target.Previous == target.Request || seen[target.Previous] || target.Restored != "" && target.Restored != target.Request {
				return installedServiceUpgrade{}, installServiceStorageError()
			}
			seen[target.Previous] = true
		}
		return plan, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return installedServiceUpgrade{}, installServiceStorageError()
	}
	records, e := store.SupersededExecutions(ctx)
	plan.Build = a.build
	for _, record := range records {
		plan.Targets = append(plan.Targets, installedServiceTarget{Previous: record.ID, Request: tasks.ID()})
	}
	if len(plan.Targets) != 0 && tasks.WritePrivate(path, plan) != nil {
		return installedServiceUpgrade{}, installServiceStorageError()
	}
	return plan, e
}

func (a *App) restoreInstalledService(ctx context.Context, streams IO, store services.Store, target installedServiceTarget, networkReady bool) (string, *protocol.Error) {
	request := services.Request{Action: "restart", ID: target.Previous, RequestID: target.Request}
	result, replayed, e := store.RecoverCompleted(ctx, request, "", network.PrepareAuto)
	if e != nil {
		return "", e
	}
	if !replayed {
		original, legacy, e := store.UpgradeRecord(ctx, target.Previous)
		if e != nil {
			return "", e
		}
		p, e := a.resolveStoredProject(original.Directory, "")
		if e != nil {
			return "", e
		}
		if p.Profile != original.Profile || p.Root != original.Directory || original.Mode == "foreground" {
			return "", protocol.NewError("instance_conflict", "The execution no longer matches its managed project identity.", 3, nil)
		}
		if original.ProxyMode && !networkReady {
			return "", protocol.NewError("proxy_pending", "Restore the listener before replacing this managed execution.", 3, nil)
		}
		if e := a.prepareServiceRestart(ctx, streams, &request, original); e != nil {
			return "", e
		}
		if legacy {
			if e := request.RestartPreflight(ctx, original, services.RestartBeforeStop); e != nil {
				return "", e
			}
			unlock, err := store.Lock(ctx)
			if err != nil {
				return "", installServiceStorageError()
			}
			e := store.StopForUpgrade(ctx, original.ID)
			unlock()
			if e != nil {
				return "", e
			}
			if e := store.ConvertUpgradeRecord(ctx, original.ID); e != nil {
				return "", protocol.WithEffects(e, "partial")
			}
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
	if live.Previous != target.Previous || live.State != "running" || live.EndedAt != nil {
		return "", protocol.NewError("process_pending", "The replacement execution did not remain running.", 3, map[string]any{"target": map[string]any{"kind": "execution", "id": live.ID, "name": live.Command}})
	}
	return live.ID, nil
}

func installServiceStorageError() *protocol.Error {
	return protocol.NewError("process_upgrade_storage", "Cannot verify the pending execution upgrade for this installed build.", 3, nil)
}
