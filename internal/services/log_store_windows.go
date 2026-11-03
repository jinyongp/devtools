package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

func acquireLogLock(ctx context.Context, logPath string, exclusive bool) (func(), error) {
	root, err := platformfs.OpenRoot(filepath.Dir(logPath), exclusive)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	flags := os.O_RDONLY
	if exclusive {
		flags = os.O_CREATE | os.O_RDWR
	}
	file, err := root.OpenFile(filepath.Base(logPath)+".lock", flags)
	if !exclusive && errors.Is(err, os.ErrNotExist) {
		return nil, &maintenance.QueryPreparationError{State: "initialization_required", Cause: err, Passive: true}
	}
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
