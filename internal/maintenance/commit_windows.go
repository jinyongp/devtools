package maintenance

import (
	"encoding/json"
	"errors"
)

// Write completes file flush, close and WRITE_THROUGH publication. Merely
// observing committed after a failed publication is not a successful commit.
func markCommitted(root string, pointer transactionPointer) (bool, error) {
	pointer.Phase = "committed"
	body, err := json.Marshal(pointer)
	if err != nil {
		return false, err
	}
	writeErr := Write(pendingPath(root), body)
	if writeErr == nil {
		return true, nil
	}
	observed, legacy, _, readErr := readPointer(root)
	if readErr != nil || legacy || observed.TransactionID != pointer.TransactionID {
		return false, errors.New("restore recovery pending")
	}
	if observed.Phase == "applying" {
		if err := recoverTransaction(root, observed); err != nil {
			return false, errors.New("restore recovery pending")
		}
		return false, writeErr
	}
	// Retain a visible committed pointer and its before-images. The next Acquire
	// verifies and cleans it without rolling back; request replay stays idempotent.
	return false, errors.New("restore recovery pending")
}
