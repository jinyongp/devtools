package services

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// RequestStatus is historical operation data, not a supervisor observation.
type RequestStatus struct {
	RequestID string  `json:"request_id"`
	State     string  `json:"state"`
	Result    *Result `json:"result,omitempty"`
}

func (s Store) InspectRequest(id string) (RequestStatus, *protocol.Error) {
	out := RequestStatus{RequestID: id, State: "pending"}
	if !validID(id) {
		return out, failure("invalid_argument")
	}
	var value receipt
	e := tasks.ReadPrivate(filepath.Join(s.root(), "receipts", id+".json"), &value)
	if errors.Is(e, os.ErrNotExist) {
		return out, protocol.NewError("request_not_found", "No saved process request exists in this store.", 3, nil)
	}
	if e != nil {
		return out, storageError(e)
	}
	digest, e := hex.DecodeString(value.Fingerprint)
	if e != nil || len(digest) != 32 || value.Done && !validID(value.Result.Item.ID) {
		return out, storageError(errors.New("invalid request receipt"))
	}
	if value.Done {
		out.State = "completed"
	}
	if value.Result.Item.ID != "" {
		out.Result = &value.Result
	}
	return out, nil
}

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
