package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
)

// Observe the existing writer pair using read handles. An absent pair requires
// explicit preparation and is never evidence that a delivery lock is free.
func TryQueryExclusive(ctx context.Context, path string) (func(), bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	preparation := func(err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return &QueryPreparationError{State: "initialization_required", Cause: err, Passive: true}
		}
		return err
	}
	root, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return nil, false, preparation(err)
	}
	defer root.Close()
	acquire := func(name string) (*platformfs.File, bool, error) {
		file, err := root.OpenFile(name, os.O_RDONLY)
		if err != nil {
			return nil, false, preparation(err)
		}
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, false, err
		}
		free, err := file.TryLock(false)
		if err != nil || !free {
			file.Close()
			return nil, false, err
		}
		return file, true, nil
	}
	turn, free, err := acquire(filepath.Base(path) + ".turnstile")
	if err != nil || !free {
		return nil, false, err
	}
	defer turn.Close()
	gate, free, err := acquire(filepath.Base(path))
	if err != nil || !free {
		return nil, false, err
	}
	return func() { _ = gate.Close() }, true, nil
}
