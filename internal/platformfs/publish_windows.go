package platformfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// PublishDirectory publishes a completed private staging directory without
// replacing an existing execution. It pins both parents, closes the staging
// directory handles before rename, and leaves the source intact on failure.
// Callers flush/close their files first and own their journal/recovery policy.
func (r *Root) PublishDirectory(ctx context.Context, from, to string) error {
	source, err := r.child(from)
	if err != nil {
		return err
	}
	target, err := r.child(to)
	if err != nil {
		return err
	}
	if source == target || source == r.path || target == r.path {
		return ErrUnsafe
	}
	sourceParent, err := guardDirectory(filepath.Dir(source), r.path, false)
	if err != nil {
		return err
	}
	defer sourceParent.close()
	targetParent, err := guardDirectory(filepath.Dir(target), r.path, false)
	if err != nil {
		return err
	}
	defer targetParent.close()
	sourcePtr, err := pathPtr(source)
	if err != nil {
		return err
	}
	targetPtr, err := pathPtr(target)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	delay := 10 * time.Millisecond
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		checked, err := guardDirectory(source, r.path, false)
		if err != nil {
			return err
		}
		if err = checked.close(); err != nil {
			return err
		}
		if _, err = os.Lstat(target); err == nil {
			return &os.PathError{Op: "publish", Path: target, Err: os.ErrExist}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		err = windows.MoveFileEx(sourcePtr, targetPtr, windows.MOVEFILE_WRITE_THROUGH)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		if delay > remaining {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay *= 2
		if delay > 200*time.Millisecond {
			delay = 200 * time.Millisecond
		}
	}
}

// PublishFile moves a closed private file between checked parents. Store phase
// transitions use this operation instead of rename followed by a fake dir fsync.
func (r *Root) PublishFile(ctx context.Context, from, to string, replace bool) error {
	source, err := r.child(from)
	if err != nil {
		return err
	}
	target, err := r.child(to)
	if err != nil {
		return err
	}
	sourceParent, err := guardDirectory(filepath.Dir(source), r.path, false)
	if err != nil {
		return err
	}
	defer sourceParent.close()
	targetParent, err := guardDirectory(filepath.Dir(target), r.path, false)
	if err != nil {
		return err
	}
	defer targetParent.close()
	return publish(ctx, source, target, replace)
}
