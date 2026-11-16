package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/installstorage"
	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/protocol"
)

const homebrewFormula = "jinyongp/tap/devtools"

func updateHomebrew(ctx context.Context, executable string) (any, *protocol.Error) {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return nil, protocol.NewError("homebrew_unavailable", "Cannot find the Homebrew installation that owns devtools.", 3, nil)
	}
	command := exec.CommandContext(ctx, brew, "--prefix", homebrewFormula)
	var output updateOutput
	command.Stdout = &output
	err = command.Run()
	prefix := strings.TrimSpace(string(output.Bytes()))
	if err != nil || output.truncated || !filepath.IsAbs(prefix) {
		return nil, protocol.NewError("homebrew_unavailable", "Cannot locate the devtools Homebrew formula.", 3, nil)
	}
	installed := filepath.Join(prefix, "bin", "devtools")
	previous, err := filepath.EvalSymlinks(installed)
	if err != nil || previous != executable {
		return nil, protocol.NewError("homebrew_installation_mismatch", "The Homebrew command on PATH does not own the running devtools executable.", 3, nil)
	}
	finish := progress.Begin(ctx, "Updating the devtools Homebrew formula")
	err = exec.CommandContext(ctx, brew, "upgrade", homebrewFormula).Run()
	finish()
	if err != nil {
		return nil, protocol.NewError("homebrew_upgrade_failed", "Homebrew did not confirm the update. Check the formula installation before resuming.", 3, map[string]any{
			"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", "update", "--resume"}, nil, "Resume the installed version if Homebrew already published it.", "value_write").Remedy(protocol.InvocationContext{})},
		})
	}
	candidate, err := filepath.EvalSymlinks(installed)
	if err != nil {
		return nil, protocol.NewError("homebrew_upgrade_failed", "Cannot locate the updated devtools executable.", 3, nil)
	}
	output = updateOutput{}
	command = exec.CommandContext(ctx, candidate, "--format", "json", "version")
	command.Stdout = &output
	var observed struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if command.Run() != nil || output.truncated || json.Unmarshal(output.Bytes(), &observed) != nil || !observed.OK || observed.Data.Version == "" {
		return nil, protocol.NewError("homebrew_upgrade_failed", "Cannot verify the updated devtools executable.", 3, nil)
	}
	changed := candidate != previous
	storage := installstorage.Run(ctx, candidate)
	didWork := changed || storage.Report != nil && storage.Report.Changed
	if proxyResult, ok := storage.Proxy.(map[string]any); ok && proxyResult["changed"] == true && proxyResult["replayed"] != true {
		didWork = true
	}
	result := map[string]any{"action": "update", "version": observed.Data.Version, "alias": map[string]any{"name": "dvt", "status": "unchanged"}, "changed": didWork, "binary_changed": changed, "storage": storage}
	return finishExecutableUpdate(result, storage, changed)
}
