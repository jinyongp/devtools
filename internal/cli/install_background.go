package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/installstorage"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/versionrange"
)

type installationReceipt struct {
	Build     string    `json:"build"`
	Attempted time.Time `json:"attempted_at"`
	Ready     bool      `json:"ready"`
}

func (a *App) canResumeInstall(receipt installationReceipt) bool {
	version, _, _ := strings.Cut(a.build, "/")
	previous, _, _ := strings.Cut(receipt.Build, "/")
	return !versionrange.Observed(version) || !versionrange.Observed(previous) || versionrange.Matches("<="+version, previous)
}

// A separate process can finish independent work without holding the pending
// user command behind the full storage inventory. Both entry points use the
// same per-root gate and the existing domain receipts.
func (a *App) scheduleInstallation(ctx context.Context) {
	version, _, _ := strings.Cut(a.build, "/")
	if !versionrange.Observed(version) || ctx.Err() != nil {
		return
	}
	dirs, err := a.currentPaths()
	if err != nil {
		return
	}
	data, e := a.dataDirectory()
	if e != nil || filepath.Dir(data) != dirs.Data {
		return
	}
	if info, err := os.Lstat(dirs.Data); err != nil || !info.IsDir() {
		return
	}
	path := filepath.Join(dirs.Data, ".installation-upgrade.json")
	var receipt installationReceipt
	err = tasks.ReadPrivate(path, &receipt)
	if err != nil && !errors.Is(err, os.ErrNotExist) || a.installationAlreadyScheduled(receipt) {
		return
	}
	release, acquired, err := maintenance.TryLockExclusive(ctx, filepath.Join(dirs.Data, ".installation-upgrade.lock"))
	if err != nil || !acquired {
		return
	}
	defer release()
	receipt = installationReceipt{}
	err = tasks.ReadPrivate(path, &receipt)
	if err != nil && !errors.Is(err, os.ErrNotExist) || a.installationAlreadyScheduled(receipt) {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	if err := tasks.WritePrivate(path, installationReceipt{Build: a.build, Attempted: time.Now().UTC()}); err != nil {
		return
	}
	command := exec.Command(executable, "__resume-installation")
	command.Env = process.Environment(os.Environ(), map[string]string{"DEVTOOLS_ISOLATED_ROOT": dirs.IsolatedRoot})
	if process.StartDetached(command) == nil {
		_ = command.Process.Release()
	}
}

func (a *App) installationAlreadyScheduled(receipt installationReceipt) bool {
	return !a.canResumeInstall(receipt) || receipt.Build == a.build && (receipt.Ready || time.Since(receipt.Attempted) < 30*time.Second)
}

func RunInstallationWorker(ctx context.Context, version, commit string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_ = New(version, commit).resumeInstallation(ctx, IO{Out: io.Discard, Err: io.Discard}, true)
}

func (a *App) resumeInstallation(ctx context.Context, streams IO, scheduled bool) installstorage.Result {
	dirs, err := a.currentPaths()
	if err != nil {
		return installstorage.Failed("storage_paths_unavailable")
	}
	path := filepath.Join(dirs.Data, ".installation-upgrade.json")
	release, err := maintenance.LockExclusive(ctx, filepath.Join(dirs.Data, ".installation-upgrade.lock"))
	if err != nil {
		return installstorage.Failed("installation_upgrade_lock_unavailable")
	}
	defer release()
	var receipt installationReceipt
	err = tasks.ReadPrivate(path, &receipt)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return installstorage.Failed("installation_upgrade_state_unavailable")
	}
	if !a.canResumeInstall(receipt) || scheduled && (receipt.Build != a.build || receipt.Ready) {
		return installstorage.Failed("installation_upgrade_superseded")
	}
	result := a.upgradeInstallation(ctx, streams)
	if err := tasks.WritePrivate(path, installationReceipt{Build: a.build, Attempted: time.Now().UTC(), Ready: result.Ready}); err != nil && result.Ready {
		result.Ready, result.Status = false, "pending"
		result.Diagnostic = installstorage.Failed("installation_upgrade_state_unavailable").Diagnostic
	}
	return result
}
