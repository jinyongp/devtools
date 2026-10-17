package ports

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) lock(ctx context.Context, name string, wait, create bool) (io.Closer, *protocol.Error) {
	root, err := platformfs.OpenRoot(s.Directory, create)
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil, nil
	}
	if err != nil {
		return nil, storageError()
	}
	defer root.Close()
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	file, err := root.OpenFile(name, flags)
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil, nil
	}
	if err != nil {
		return nil, storageError()
	}
	for {
		if ctx.Err() != nil {
			_ = file.Close()
			return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		acquired, err := file.TryLock(false)
		if err != nil {
			_ = file.Close()
			return nil, storageError()
		}
		if acquired {
			return file, nil
		}
		if !wait {
			_ = file.Close()
			return nil, fail("port_run_active")
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
