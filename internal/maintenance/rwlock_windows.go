package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func ensurePrivateLockDirectory(path string) error {
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		return err
	}
	return root.Close()
}
func acquireFileLock(ctx context.Context, path string, mode lockMode) (*lockedFile, error) {
	root, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(filepath.Base(path), os.O_RDWR|os.O_CREATE)
	_ = root.Close()
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		acquired, err := file.TryLock(mode == lockShared)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if acquired {
			return &lockedFile{File: file.File, closeGuard: file.Close}, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func unlockFile(file *lockedFile) {
	if file == nil {
		return
	}
	// Closing the last handle releases this ordinary byte-range lock. Explicit
	// unlock and guard close also make failure paths deterministic.
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
	_ = file.Close()
}
