package lifecycle

import (
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/protocol"
)

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
