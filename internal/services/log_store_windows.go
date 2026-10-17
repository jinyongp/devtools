package services

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func acquireLogLock(ctx context.Context, logPath string, exclusive bool) (func(), error) {
	root, err := platformfs.OpenRoot(filepath.Dir(logPath), true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.OpenFile(filepath.Base(logPath)+".lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		acquired, err := file.TryLock(!exclusive)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if acquired {
			return func() { _ = file.Unlock(); _ = file.Close() }, nil
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
