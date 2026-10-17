//go:build !windows

package maintenance

import (
	"errors"

	"github.com/jinyongp/devtools/internal/platformfs"
	"io"
	"os"
	"path/filepath"
)

func createTransactionDirectories(root, id string) (string, string, error) {
	maintenanceDir := filepath.Join(root, ".maintenance")
	if err := privateDirectoryComponent(maintenanceDir, false); err != nil {
		return "", "", err
	}
	transactions := filepath.Join(maintenanceDir, "transactions")
	if err := privateDirectoryComponent(transactions, true); err != nil {
		return "", "", err
	}
	dir := filepath.Join(transactions, id)
	if err := platformfs.MkdirPrivate(dir); err != nil {
		return "", "", err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
			_ = syncDir(transactions)
		}
	}()
	if err := privateDirectoryComponent(dir, false); err != nil {
		return "", "", err
	}
	before := filepath.Join(dir, "before")
	if err := platformfs.MkdirPrivate(before); err != nil {
		return "", "", err
	}
	if err := privateDirectoryComponent(before, false); err != nil {
		return "", "", err
	}
	if err := syncDir(transactions); err != nil {
		return "", "", err
	}
	if err := syncDir(dir); err != nil {
		return "", "", err
	}
	success = true
	return dir, before, nil
}

func restorePrivateFile(source, target string) error {
	parent := filepath.Dir(target)
	temp, err := os.CreateTemp(parent, ".restore-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	src, err := platformfs.OpenPrivate(source, os.O_RDONLY, 0)
	if err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := privateFileInfo(src); err != nil {
		_ = src.Close()
		_ = temp.Close()
		return err
	}
	_, copyErr := io.Copy(temp, src)
	closeSrcErr := src.Close()
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeTempErr := temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSrcErr != nil {
		return closeSrcErr
	}
	if closeTempErr != nil {
		return closeTempErr
	}
	if err := os.Rename(tempPath, target); err != nil {
		return err
	}
	return syncDir(parent)
}

func removeTarget(target string) error {
	err := os.Remove(target)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return err
	}
	return syncDir(filepath.Dir(target))
}

func cleanupTransaction(root string, pointer transactionPointer) error {
	pending := pendingPath(root)
	observed, legacy, raw, err := readPointer(root)
	if err != nil {
		return err
	}
	if raw != nil {
		if legacy || observed.TransactionID != pointer.TransactionID || observed.Phase != pointer.Phase {
			return errors.New("unexpected recovery pointer")
		}
		if err := os.Remove(pending); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := syncDir(filepath.Dir(pending)); err != nil {
			return err
		}
	}
	dir := transactionDir(root, pointer.TransactionID)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(transactionsRoot(root)); err == nil && info.IsDir() {
		return syncDir(transactionsRoot(root))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func cleanupOrphanTransactions(root, active string) error {
	parent := transactionsRoot(root)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !platformfs.DirectoryPrivate(parent, info) {
		return errors.New("invalid transaction directory")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		// A previous cleanup may have removed the pointer but failed to sync
		// that removal. Checkpoint its directory before discarding before-images.
		if err := syncDir(filepath.Dir(pendingPath(root))); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if entry.Name() == active {
			continue
		}
		if !validTransactionID(entry.Name()) {
			return errors.New("invalid transaction directory")
		}
		path := filepath.Join(parent, entry.Name())
		if err := privateDirectoryComponent(path, false); err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return syncDir(parent)
}
