package vault

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/platformfs"
)

// WriteIdentity pins the protected key slot and durable manual-lock state.
// It performs no IPC or native key lookup. A commit also holds the shared
// key-slot-write lease so a rewrap cannot cross its identity check/publication.
func (s KeyFileStore) WriteIdentity() ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	k, err := s.Read()
	if errors.Is(err, ErrKeyMissing) {
		return zero, nil
	}
	if err != nil {
		return zero, err
	}
	r, err := platformfs.OpenRoot(s.Directory, false)
	if err != nil {
		return zero, ErrRecovery
	}
	defer r.Close()
	raw, err := r.ReadFile("manual-lock.json", MaxKeySlotBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return zero, ErrRecovery
	}
	if err == nil {
		var latch manualLock
		if StrictDecode(raw, &latch) != nil || latch.Format != StorageVersion || latch.VaultID != k.VaultID {
			return zero, ErrRecovery
		}
		if latch.Locked {
			return zero, ErrLocked
		}
	}
	encoded, _ := json.Marshal(k)
	encoded = append(encoded, raw...)
	return sha256.Sum256(encoded), nil
}
