//go:build !windows

package maintenance

import (
	"encoding/json"
	"errors"

	"path/filepath"
)

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
