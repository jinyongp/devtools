package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

const runtimeTransitionFile = VersionedRuntime + "/transition"
const runtimeClosing = "closing\n"
const runtimeDraining = "draining\n"
const runtimeReady = "ready\n"

var runtimeDomains = []string{"ports", "processes", "project-operations", "network-operations", "proxy", "exposures"}

var ErrRuntimeTransitionPending = errors.New("runtime storage transition pending")
var ErrLegacyRuntimeRecreated = errors.New("legacy runtime directory recreated during transition")

// RuntimeTransitionState reports whether the legacy runtime cutover was
// published, or an existing cutover needs to be resumed.
func RuntimeTransitionState(root string) (ready, pending bool, err error) {
	phase, err := runtimeTransitionPhase(root)
	if err != nil {
		return false, false, err
	}
	if phase == runtimeReady {
		err := verifyRuntimeFences(root)
		return err == nil, false, err
	}
	return false, phase != "", nil
}

func AcquireRuntimeTransition(ctx context.Context, root string) (func(), error) {
	release, err := AcquireForRecovery(ctx, root)
	if err != nil {
		return nil, err
	}
	if err := recoverPending(root); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func runtimeTransitionPhase(root string) (string, error) {
	file := filepath.Join(root, runtimeTransitionFile)
	if _, err := os.Lstat(file); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if err := platformfs.CheckDirectory(filepath.Dir(file)); err != nil {
		return "", err
	}
	body, err := Read(file, 128)
	if err != nil {
		return "", err
	}
	switch phase := string(body); phase {
	case runtimeClosing, runtimeDraining, runtimeReady:
		return phase, nil
	}
	return "", errors.New("invalid runtime storage transition")
}

func checkRuntimeTransition(root string) error {
	phase, err := runtimeTransitionPhase(root)
	if err != nil {
		return err
	}
	if phase == runtimeClosing || phase == runtimeDraining {
		return ErrRuntimeTransitionPending
	}
	if phase == runtimeReady && RecoveryPointerRelative(root) != fencedRecoveryPointer {
		return errors.New("legacy maintenance fence is missing")
	}
	return nil
}

// StageRuntimeTransitionHeld durably closes legacy paths and leaves the store
// pending. The caller owns the exclusive maintenance and execution gates. It
// must drain already-admitted operations before publishing the new layout.
func StageRuntimeTransitionHeld(ctx context.Context, root string) error {
	phase, err := runtimeTransitionPhase(root)
	if err != nil {
		return err
	}
	if phase == runtimeReady {
		return verifyRuntimeFences(root)
	}
	if err := recoverPending(root); err != nil {
		return err
	}
	handle, err := platformfs.OpenRoot(root, false)
	if err != nil {
		return err
	}
	defer handle.Close()
	if phase == "" {
		if err := handle.MkdirAll(VersionedRuntime); err != nil {
			return err
		}
		if err := handle.WriteFile(ctx, runtimeTransitionFile, []byte(runtimeClosing), false); err != nil {
			return err
		}
	}
	if err := FenceLegacyRecoveryHeld(root); err != nil {
		return err
	}
	for _, domain := range runtimeDomains {
		if err := closeRuntimePath(ctx, handle, root, domain); err != nil {
			return err
		}
	}
	return handle.WriteFile(ctx, runtimeTransitionFile, []byte(runtimeDraining), true)
}

func closeRuntimePath(ctx context.Context, handle *platformfs.Root, root, domain string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	legacy := filepath.Join(root, domain)
	staged := filepath.Join(VersionedRuntime, domain)
	stagedPath := filepath.Join(root, staged)
	if hasRuntimeFence(root, domain) {
		if _, err := os.Lstat(stagedPath); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return platformfs.CheckDirectory(stagedPath)
	}
	info, err := os.Lstat(legacy)
	if err == nil {
		if !info.IsDir() {
			return errors.New("unexpected legacy runtime path")
		}
		if err := platformfs.CheckDirectory(legacy); err != nil {
			return err
		}
		if _, err := os.Lstat(stagedPath); err == nil {
			return fmt.Errorf("%w: %s", ErrLegacyRuntimeRecreated, domain)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := stageRuntimeDirectory(ctx, handle, root, domain, staged); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if _, err := os.Lstat(stagedPath); err == nil {
		if err := platformfs.CheckDirectory(stagedPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return handle.WriteFile(ctx, domain, []byte(RuntimeFenceDocument), false)
}

// PublishRuntimeTransitionHeld makes the new layout visible after verifying
// that no operation admitted through legacy paths still owns a writer.
// verifyDrained must inspect the physical layout without taking either gate.
func PublishRuntimeTransitionHeld(ctx context.Context, root string, verifyDrained func(context.Context, string) error) error {
	phase, err := runtimeTransitionPhase(root)
	if err != nil {
		return err
	}
	if phase == runtimeReady {
		return verifyRuntimeFences(root)
	}
	if phase != runtimeDraining || verifyDrained == nil {
		return ErrRuntimeTransitionPending
	}
	if err := verifyRuntimeFences(root); err != nil {
		return err
	}
	if err := verifyDrained(ctx, filepath.Join(root, VersionedRuntime)); err != nil {
		return err
	}
	handle, err := platformfs.OpenRoot(root, false)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.WriteFile(ctx, runtimeTransitionFile, []byte(runtimeReady), true)
}

func verifyRuntimeFences(root string) error {
	if RecoveryPointerRelative(root) != fencedRecoveryPointer {
		return errors.New("legacy maintenance fence is missing")
	}
	for _, domain := range runtimeDomains {
		if !hasRuntimeFence(root, domain) {
			return errors.New("legacy runtime fence is missing")
		}
	}
	return nil
}
