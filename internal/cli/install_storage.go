package cli

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/dashboard"
	"github.com/jinyongp/devtools/internal/installstorage"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

// RunInstallStorage is the installer's private, noninteractive entry point. It
// resumes the installed environment with no backend selection or unlock.
func RunInstallStorage(ctx context.Context, output io.Writer, version, commit string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	app := New(version, commit)
	result := installstorage.Failed("storage_paths_unavailable")
	directories, err := app.currentPaths()
	if err == nil && directories.IsolatedRoot == "" {
		result = app.resumeInstallation(ctx, IO{Out: io.Discard, Err: io.Discard})
	} else if err == nil {
		result = installstorage.Failed("isolated_operation_not_allowed")
	}
	return json.NewEncoder(output).Encode(result.Compact())
}

func (a *App) resumeInstallation(ctx context.Context, streams IO) installstorage.Result {
	directories, err := a.currentPaths()
	if err != nil {
		return installstorage.Failed("storage_paths_unavailable")
	}
	manager, e := a.proxyStore()
	if e != nil {
		return installstorage.Failed(e.Code)
	}
	hostManager, e := a.hostsManager(ctx, streams)
	if e != nil {
		return installstorage.Failed(e.Code)
	}
	proxyResult, networkFailure := manager.UpgradeNetwork(ctx, manager.UpgradeRequestID(), hostManager)
	processes, processFailure := a.upgradeInstalledServices(ctx, streams, networkFailure == nil)
	report, e := a.storageEngine(directories.Data).Fix(ctx)
	result := installationStorageResult(report, e)
	result.Proxy = proxyResult
	result.Processes = processes
	result.RuntimeChanged = len(processes) > 0
	if result.Ready {
		outcome, e := dashboard.UpgradeWithRoot(ctx, directories.Cache, filepath.Join(directories.Data, "tasks"), directories.IsolatedRoot, a.build)
		if e != nil {
			result.Ready, result.Status = false, "pending"
			result.Diagnostic = protocol.ObservationDiagnostic(e)
		} else {
			result.Dashboard = outcome
			result.RuntimeChanged = result.RuntimeChanged || outcome["changed"] == true
		}
	}
	for _, failure := range []*protocol.Error{networkFailure, processFailure} {
		if failure != nil {
			if result.Ready {
				result.Ready, result.Status = false, "pending"
			}
			if result.Diagnostic == nil {
				result.Diagnostic = protocol.ObservationDiagnostic(failure)
			}
		}
	}
	if proxyResult, ok := proxyResult.(map[string]any); ok {
		if proxyResult["changed"] == true && proxyResult["replayed"] != true {
			result.RuntimeChanged = true
		}
		if exposures, ok := proxyResult["exposures"].([]map[string]any); ok {
			for _, exposure := range exposures {
				if exposure["state"] != "restored" && exposure["state"] != "closed" {
					result.Ready, result.Status = false, "pending"
				}
			}
		}
	}
	return result
}

func installationStorageResult(report storageupgrade.Report, failure *protocol.Error) installstorage.Result {
	result := installstorage.Result{Status: "ready", Ready: report.Ready && failure == nil, Report: &report, Diagnostic: protocol.ObservationDiagnostic(failure)}
	if result.Ready {
		return result
	}
	result.Status = "blocked"
	if failure != nil {
		result.Status = "failed"
	}
	for _, step := range report.Items {
		switch step.Reason {
		case "vault_locked", "vault_locked_manual", "key_missing", "initialization_required", "recovery_required", "protection_required":
			result.Status = "pending"
		}
	}
	return result
}

func storageUpdateSchema() map[string]any {
	return object(map[string]any{"status": map[string]any{"enum": []string{"ready", "pending", "blocked", "failed"}}, "ready": map[string]any{"type": "boolean"}, "report": configcontract.Definition("StorageUpgradeReport"), "diagnostic": configcontract.Definition("ObservationDiagnostic"), "proxy": map[string]any{"type": "object"}, "dashboard": map[string]any{"type": "object"}, "processes": arraySchema(object(map[string]any{"previous_id": stringSchema(), "execution_id": stringSchema()}, "previous_id", "execution_id")), "runtime_changed": boolSchema()}, "status", "ready")
}

func finishExecutableUpdate(result any, storage installstorage.Result, binaryChanged bool) (any, *protocol.Error) {
	if storage.Ready {
		return result, nil
	}
	effects := "unknown"
	if storage.Report != nil {
		effects = storage.Report.Effects
		if (binaryChanged || storage.RuntimeChanged) && effects == "none" {
			effects = "committed"
		}
	}
	return nil, protocol.NewError("storage_upgrade_"+storage.Status, "The executable is installed; environment upgrade requires attention. Resolve the reported prerequisite and run devtools update --resume.", 3, map[string]any{
		"phase": "observe", "reason": "storage_upgrade_" + storage.Status, "effects": effects, "retry": "after_dependency_change", "report": result,
		"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", "update", "--resume"}, nil, "Resume the environment upgrade with the installed executable.", "value_write").Remedy(protocol.InvocationContext{})},
	})
}
