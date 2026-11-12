package maintenance

import (
	"context"
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func probeRuntimeWriter(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return ErrRuntimeWriterActive
	}
	if err != nil {
		return err
	}
	defer file.Close()
	free, err := file.TryLock(false)
	if err == nil && !free {
		return ErrRuntimeWriterActive
	}
	return err
}
