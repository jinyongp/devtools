package maintenance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func prepareTransaction(root string, files map[string][]byte) (transactionPointer, error) {
	var pointer transactionPointer
	id, err := transactionID()
	if err != nil {
		return pointer, err
	}
	dir, beforeDir, err := createTransactionDirectories(root, id)
	if err != nil {
		return pointer, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
			_ = syncDir(transactionsRoot(root))
		}
	}()

	paths := make([]string, 0, len(files))
	for relative := range files {
		if !validReplacementPath(relative) {
			return pointer, errors.New("invalid replacement path")
		}
		paths = append(paths, relative)
	}
	sort.Strings(paths)

	manifest := transactionManifest{Version: transactionVersion, TransactionID: id, Entries: make([]transactionEntry, 0, len(paths))}
	for index, relative := range paths {
		target, err := targetPath(root, relative, true)
		if err != nil {
			return pointer, err
		}
		beforeName := fmt.Sprintf("before-%04d", index)
		size, digest, err := copyPrivateFile(target, filepath.Join(beforeDir, beforeName))
		if errors.Is(err, os.ErrNotExist) {
			manifest.Entries = append(manifest.Entries, transactionEntry{Path: relative})
			continue
		}
		if err != nil {
			return pointer, err
		}
		manifest.Entries = append(manifest.Entries, transactionEntry{
			Path:       relative,
			Exists:     true,
			BeforePath: beforeName,
			Size:       size,
			Digest:     digest,
		})
	}
	if err := syncDir(beforeDir); err != nil {
		return pointer, err
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return pointer, err
	}
	if err := Write(filepath.Join(dir, "manifest.json"), body); err != nil {
		return pointer, err
	}
	pointer = transactionPointer{Version: transactionVersion, TransactionID: id, Phase: "applying"}
	body, err = json.Marshal(pointer)
	if err != nil {
		return transactionPointer{}, err
	}
	if writeErr := Write(pendingPath(root), body); writeErr != nil {
		observed, legacy, raw, readErr := readPointer(root)
		if readErr == nil && raw == nil {
			return transactionPointer{}, writeErr
		}
		if readErr != nil || legacy || observed.TransactionID != pointer.TransactionID {
			cleanup = false
			return transactionPointer{}, errors.New("restore recovery pending")
		}
		if observed.Phase != "applying" {
			cleanup = false
			return transactionPointer{}, errors.New("restore recovery pending")
		}
		cleanup = false
		if rollbackErr := recoverTransaction(root, observed); rollbackErr != nil {
			return transactionPointer{}, errors.New("restore recovery pending")
		}
		return transactionPointer{}, writeErr
	}
	cleanup = false
	return pointer, nil
}

func applyReplacement(root string, files map[string][]byte) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, relative := range paths {
		target, err := targetPath(root, relative, true)
		if err != nil {
			return err
		}
		data := files[relative]
		if data == nil {
			if err := removeTarget(target); err != nil {
				return err
			}
			continue
		}
		if err := Write(target, data); err != nil {
			return err
		}
	}
	return nil
}

func markCommitted(root string, pointer transactionPointer) (bool, error) {
	return markCommittedWithIO(root, pointer, Write, syncDir)
}

func markCommittedWithIO(root string, pointer transactionPointer, write func(string, []byte) error, sync func(string) error) (bool, error) {
	pointer.Phase = "committed"
	body, err := json.Marshal(pointer)
	if err != nil {
		return false, err
	}
	writeErr := write(pendingPath(root), body)
	if writeErr == nil {
		return true, nil
	}
	observed, legacy, _, readErr := readPointer(root)
	if readErr != nil || legacy || observed.TransactionID != pointer.TransactionID {
		return false, errors.New("restore recovery pending")
	}
	switch observed.Phase {
	case "committed":
		// A visible rename is not proof of durability. Establish the directory
		// sync before permitting success and before-image cleanup.
		if err := sync(filepath.Dir(pendingPath(root))); err != nil {
			return false, errors.New("restore recovery pending")
		}
		return true, nil
	case "applying":
		if rollbackErr := recoverTransaction(root, observed); rollbackErr != nil {
			return false, errors.New("restore recovery pending")
		}
		return false, writeErr
	default:
		return false, errors.New("restore recovery pending")
	}
}
