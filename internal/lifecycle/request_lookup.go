package lifecycle

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type RequestStatus struct {
	RequestID string  `json:"request_id"`
	State     string  `json:"state"`
	Result    *Result `json:"result,omitempty"`
}

// InspectRequest never resumes children, checks readiness or prepares a network.
func (m Manager) InspectRequest(id string) (RequestStatus, *protocol.Error) {
	out := RequestStatus{RequestID: id, State: "pending"}
	if !requestIDPattern.MatchString(id) {
		return out, fail("invalid_argument", "A request UUID is required.", 2, nil)
	}
	value, err := m.readReceipt(id)
	if errors.Is(err, os.ErrNotExist) {
		return out, fail("request_not_found", "No saved project request exists in this store.", 3, nil)
	}
	if err != nil {
		return out, storageError(err)
	}
	digest, err := hex.DecodeString(value.Fingerprint)
	if err != nil || len(digest) != 32 || !filepath.IsAbs(value.Directory) || !project.ValidProfile(value.Profile) || value.Action != ActionUp && value.Action != ActionDown && value.Action != ActionRestart {
		return out, storageError(errors.New("invalid request receipt"))
	}
	if value.complete() {
		out.State = "completed"
	}
	result := value.result(false)
	out.Result = &result
	return out, nil
}

func (m Manager) HasRequest(id string) (bool, *protocol.Error) {
	if !requestIDPattern.MatchString(id) {
		return false, fail("invalid_argument", "A request UUID is required.", 2, nil)
	}
	_, err := m.readReceipt(id)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, storageError(err)
	}
	return true, nil
}
