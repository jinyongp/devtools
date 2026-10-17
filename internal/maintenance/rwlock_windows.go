package maintenance

import (
	"context"
	"errors"
	"os"
)

// The native lock backend belongs to W1L. Never substitute a no-op lock.
func ensurePrivateLockDirectory(path string) error { return errors.ErrUnsupported }
func acquireFileLock(ctx context.Context, path string, mode lockMode) (*os.File, error) {
	return nil, errors.ErrUnsupported
}
func unlockFile(file *os.File) {
	if file != nil {
		_ = file.Close()
	}
}
