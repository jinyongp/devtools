//go:build !windows

package maintenance

import (
	"context"
	"errors"
	"os"
	"syscall"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func probeRuntimeWriter(ctx context.Context, path string) error {
	file, err := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrRuntimeWriterActive
		}
		return err
	}
}
