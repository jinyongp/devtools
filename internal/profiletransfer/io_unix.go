//go:build !windows

package profiletransfer

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if err := privateDirectory(path); err != nil {
		return err
	}
	// Persist the directory entry too: syncing the identity's containing
	// directory alone cannot make a newly created transfer directory durable.
	// Repeat this on reuse so a previous uncertain publication can finish.
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	return syncDirectory(path)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func stageIdentity(path string, body []byte) error {
	file, err := platformfs.OpenNoFollow(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		_ = syncDirectory(filepath.Dir(path))
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		_ = syncDirectory(filepath.Dir(path))
		return closeErr
	}
	return syncDirectory(filepath.Dir(path))
}

func publishIdentity(pending, target string) error {
	if err := os.Link(pending, target); err != nil {
		return err
	}
	directory := filepath.Dir(target)
	if err := syncDirectory(directory); err != nil {
		return err
	}
	if err := os.Remove(pending); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func reconcilePendingIdentity(pending, target string) error {
	pendingIdentity, err := readIdentity(pending)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || pendingIdentity == nil {
		return ErrInvalidIdentity
	}
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return ErrInvalidIdentity
	}
	pendingInfo, err := os.Lstat(pending)
	if err != nil || !os.SameFile(targetInfo, pendingInfo) {
		return ErrInvalidIdentity
	}
	if err := os.Remove(pending); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}
