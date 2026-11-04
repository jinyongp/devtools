package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func acquirePassivePair(ctx context.Context, root string) (func(), error) {
	return acquirePassiveFilePair(ctx, filepath.Join(root, ".maintenance", "lock"))
}

func acquirePassiveFilePair(ctx context.Context, path string) (func(), error) {
	directory, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	acquire := func(name string) (*platformfs.File, error) {
		var finishWait func()
		defer func() {
			if finishWait != nil {
				finishWait()
			}
		}()
		file, err := directory.OpenFile(name, os.O_RDONLY)
		if err != nil {
			return nil, err
		}
		for {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, err
			}
			locked, err := file.TryLock(true)
			if err != nil {
				_ = file.Close()
				return nil, err
			}
			if locked {
				return file, nil
			}
			if finishWait == nil {
				finishWait = reportLockWait(ctx, path)
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
	turn, err := acquire(filepath.Base(path) + ".turnstile")
	if err != nil {
		return nil, err
	}
	gate, err := acquire(filepath.Base(path))
	_ = turn.Close()
	if err != nil {
		return nil, err
	}
	return func() { _ = gate.Close() }, nil
}
