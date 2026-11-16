package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/protocol"
)

// Set by package-manager builds to preserve ownership of installed files.
var updateManager string

type updateOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *updateOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func (a *App) registerUpdate(version string) {
	a.commands = append(a.commands, Command{
		Name: "update", Description: "Update the installed executable while preserving user data.",
		Options: []Option{{Name: "version", Description: "Release version; defaults to latest stable.", Pattern: `^0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`}, {Name: "source", Description: "Local release directory or HTTPS artifact directory; requires version.", MinLength: 1}, {Name: "resume", Boolean: true, Description: "Complete the environment upgrade using the installed executable."}},
		Output: object(map[string]any{
			"changed":        map[string]any{"type": "boolean"},
			"binary_changed": map[string]any{"type": "boolean"},
			"storage":        storageUpdateSchema(),
			"action":         stringSchema(),
			"version":        stringSchema(),
			"alias": object(map[string]any{
				"name":   stringSchema(),
				"status": map[string]any{"enum": []string{"created", "unchanged", "skipped_conflict", "failed"}},
			}, "name", "status"),
		}, "action", "version", "alias", "changed", "binary_changed", "storage"),
		Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			if r.Options["resume"] == "true" {
				if r.Options["version"] != "" || r.Options["source"] != "" {
					return nil, argumentError("--resume uses the installed version and excludes --version and --source.", "resume")
				}
				storage := a.resumeInstallation(ctx, streams, false).Compact()
				changed := storage.RuntimeChanged || storage.Report != nil && storage.Report.Changed
				return finishExecutableUpdate(map[string]any{"action": "update", "version": version, "alias": map[string]any{"name": "dvt", "status": "unchanged"}, "changed": changed, "binary_changed": false, "storage": storage}, storage, false)
			}
			directories, pathErr := a.currentPaths()
			if pathErr != nil {
				return nil, portFailure("io_error")
			}
			if directories.IsolatedRoot != "" {
				return nil, protocol.NewError("isolated_operation_not_allowed", "Installed executable updates are unavailable in isolated mode.", 3, nil)
			}
			if r.Options["source"] != "" && r.Options["version"] == "" {
				return nil, argumentError("Source requires an explicit version.", "version")
			}
			executable, err := os.Executable()
			if err == nil {
				executable, err = filepath.EvalSymlinks(executable)
			}
			if err != nil || !installedExecutableName(filepath.Base(executable)) {
				return nil, protocol.NewError("update_failed", "Update requires an installed devtools executable.", 1, nil)
			}
			return updateExecutable(ctx, executable, r.Options, streams)
		},
	})
}

func updateExecutable(ctx context.Context, executable string, options map[string]string, streams ...IO) (any, *protocol.Error) {
	if updateManager == "homebrew" {
		if options["version"] == "" && options["source"] == "" {
			return updateHomebrew(ctx, executable)
		}
		return nil, protocol.NewError("package_managed", "Update this Homebrew installation with brew upgrade jinyongp/tap/devtools, then run devtools update --resume.", 3, map[string]any{"manager": "homebrew", "command": "brew upgrade jinyongp/tap/devtools", "remedies": []protocol.Remedy{{Argv: []string{"brew", "upgrade", "jinyongp/tap/devtools"}, RequiredInputs: []string{}, Message: "Update with the package manager that owns this installation.", Effect: "system_write"}, {Argv: []string{"devtools", "update", "--resume"}, RequiredInputs: []string{}, Message: "Complete the environment upgrade with the installed executable.", Effect: "value_write"}}})
	}
	defer progress.Begin(ctx, "Updating the executable and preparing storage")()
	return updatePlatform(ctx, executable, options, streams...)
}
