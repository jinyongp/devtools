package cli

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/installstorage"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

// RunInstallStorage is the installer's private, noninteractive entry point. It
// calls exactly the doctor coordinator, with no backend selection or unlock.
func RunInstallStorage(ctx context.Context, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	app := New("", "")
	result := installstorage.Failed("storage_paths_unavailable")
	directories, err := app.currentPaths()
	if err == nil && directories.IsolatedRoot == "" {
		report, failure := app.storageEngine(directories.Data).Fix(ctx)
		result = installationStorageResult(report, failure)
	} else if err == nil {
		result = installstorage.Failed("isolated_operation_not_allowed")
	}
	return json.NewEncoder(output).Encode(result)
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
	return object(map[string]any{"status": map[string]any{"enum": []string{"ready", "pending", "blocked", "failed"}}, "ready": map[string]any{"type": "boolean"}, "report": configcontract.Definition("StorageUpgradeReport"), "diagnostic": configcontract.Definition("ObservationDiagnostic")}, "status", "ready")
}

func finishExecutableUpdate(result any, storage installstorage.Result, binaryChanged bool) (any, *protocol.Error) {
	if storage.Ready {
		return result, nil
	}
	effects := "unknown"
	if storage.Report != nil {
		effects = storage.Report.Effects
		if binaryChanged && effects == "none" {
			effects = "committed"
		}
	}
	return nil, protocol.NewError("storage_upgrade_"+storage.Status, "The executable is installed; storage requires attention. Resolve the reported prerequisite and run doctor --storage --fix.", 3, map[string]any{
		"phase": "observe", "reason": "storage_upgrade_" + storage.Status, "effects": effects, "retry": "after_dependency_change", "report": result,
		"remedies": []protocol.Remedy{{Argv: []string{"devtools", "doctor", "--storage", "--fix"}, Effect: "value_write", RequiredInputs: []string{}, Message: "Resolve storage prerequisites and resume with the installed executable."}},
	})
}
