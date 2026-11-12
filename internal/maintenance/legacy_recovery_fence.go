package maintenance

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

const legacyRecoveryPointer = ".maintenance/restore-pending.json"
const fencedRecoveryPointer = ".maintenance/restore-pending-v26.json"
const legacyRecoveryFence = `{"version":5,"phase":"legacy-write-closed"}`

// RecoveryPointerRelative is shared by ordinary rollback and protected forward
// recovery. Fenced stores leave the legacy pointer occupied permanently so an
// old caller that already opened the gate still observes an unsupported format.
func RecoveryPointerRelative(root string) string {
	path := filepath.Join(root, legacyRecoveryPointer)
	info, err := os.Lstat(path)
	if err == nil && info.Size() == int64(len(legacyRecoveryFence)) {
		body, err := Read(path, int64(len(legacyRecoveryFence)))
		if err == nil && bytes.Equal(body, []byte(legacyRecoveryFence)) {
			return fencedRecoveryPointer
		}
	}
	return legacyRecoveryPointer
}

// FenceLegacyRecoveryHeld closes the legacy maintenance protocol while
// preserving the current protected-root identity. The caller owns the
// exclusive gate and coordinates independent runtime writers separately.
func FenceLegacyRecoveryHeld(root string) error {
	if RecoveryPointerRelative(root) == fencedRecoveryPointer {
		return nil
	}
	pointer, legacy, body, err := readPointer(root)
	if err != nil {
		return err
	}
	if legacy || body != nil && (pointer.Version != 3 || pointer.Phase != "ready") {
		return errors.New("pending recovery must finish before closing legacy maintenance")
	}
	stagedPath := filepath.Join(root, fencedRecoveryPointer)
	existing, err := Read(stagedPath, 4096)
	switch {
	case err == nil:
		if body == nil || !bytes.Equal(body, existing) {
			return errors.New("conflicting staged recovery pointer")
		}
	case errors.Is(err, os.ErrNotExist):
		if body != nil {
			if err := Write(stagedPath, body); err != nil {
				return err
			}
		}
	default:
		return err
	}
	return Write(filepath.Join(root, legacyRecoveryPointer), []byte(legacyRecoveryFence))
}
