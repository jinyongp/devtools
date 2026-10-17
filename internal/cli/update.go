package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
)

// Set by package-manager builds to preserve ownership of installed files.
var updateManager string

func (a *App) registerUpdate() {
	a.commands = append(a.commands, Command{
		Name: "update", Description: "Update the installed executable while preserving user data.",
		Options: []Option{{Name: "version", Description: "Release version; defaults to latest stable.", Pattern: `^0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`}, {Name: "source", Description: "Local release directory or HTTPS artifact directory; requires version.", MinLength: 1}},
		Output: object(map[string]any{
			"changed": map[string]any{"type": "boolean"},
			"action":  stringSchema(),
			"version": stringSchema(),
			"alias": object(map[string]any{
				"name":   stringSchema(),
				"status": map[string]any{"enum": []string{"created", "unchanged", "skipped_conflict", "failed"}},
			}, "name", "status"),
		}, "action", "version", "alias", "changed"),
		Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
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
		return nil, protocol.NewError("package_managed", "Update this Homebrew installation with brew upgrade jinyongp/tap/devtools.", 3, map[string]any{"manager": "homebrew", "command": "brew upgrade jinyongp/tap/devtools"})
	}
	return updatePlatform(ctx, executable, options, streams...)
}
