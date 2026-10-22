package services

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// HasRequest lets initiating CLIs retain legacy request semantics before doing
// any new network preparation. Apply remains the authority for replay and hashes.
func (s Store) HasRequest(id string) (bool, *protocol.Error) {
	if !validID(id) {
		return false, failure("invalid_argument")
	}
	var value receipt
	e := tasks.ReadPrivate(filepath.Join(s.root(), "receipts", id+".json"), &value)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, storageError(e)
	}
	return true, nil
}
