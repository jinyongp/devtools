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
func (r *Root) PublishDirectory(ctx context.Context, from, to string) (err error) {
	defer func() { err = fsError("publish_directory", to, "", err) }()
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
		checked, checkErr := guardDirectory(source, r.path, false)
		err = checkErr
		if err == nil {
			err = checked.close()
		}
		if err == nil {
			if _, statErr := os.Lstat(target); statErr == nil {
				return &os.PathError{Op: "publish", Path: target, Err: os.ErrExist}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				err = statErr
			}
		}
		if err == nil {
			err = windows.MoveFileEx(sourcePtr, targetPtr, windows.MOVEFILE_WRITE_THROUGH)
		}
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

// RemoveFile checks the private file and pins its parents through bounded deletion.
func (r *Root) RemoveFile(ctx context.Context, name string) (err error) {
	defer func() { err = fsError("remove_file", name, "", err) }()
	path, err := r.child(name)
	if err != nil {
		return err
	}
	guard, err := guardDirectory(filepath.Dir(path), r.path, false)
	if err != nil {
		return err
	}
	defer guard.close()
	return removeGuardedFile(ctx, path)
}
func removeGuardedFile(ctx context.Context, path string) error {
	ptr, err := pathPtr(path)
	if err != nil {
		return err
	}
	deadline, delay := time.Now().Add(2*time.Second), 10*time.Millisecond
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		f, e := openPrivateFile(path, os.O_RDONLY)
		err = e
		if err == nil {
			err = f.Close()
		}
		if err == nil {
			err = windows.DeleteFile(ptr)
		}
		if err == nil || (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED)) {
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
